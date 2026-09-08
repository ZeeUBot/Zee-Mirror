package api

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"zee-mirror/internal/config"
	"zee-mirror/internal/domain"
	"zee-mirror/internal/metrics"
	"zee-mirror/internal/repository"
	"zee-mirror/internal/router"
	"zee-mirror/internal/service"
	"zee-mirror/plugins/torrent"

	_ "zee-mirror/docs" // swagger docs init

	"github.com/getsentry/sentry-go"
	"github.com/golang-jwt/jwt/v5"
	httpSwagger "github.com/swaggo/http-swagger"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

type Server struct {
	Service    *service.BotService
	Hub        *Hub
	Router     *router.Router
	httpServer *http.Server
	Port       int
}

func NewServer(service *service.BotService, port int) *Server {
	return &Server{
		Service: service,
		Port:    port,
		Hub:     NewHub(),
	}
}

func (s *Server) SetRouter(r *router.Router) {
	s.Router = r
}

func jwtKey(secret string) []byte {
	h := sha256.Sum256([]byte(secret))
	return h[:]
}

// validDashboardToken accepts the raw dashboard token or a JWT signed with it,
// so REST and websocket share one auth seam.
func (s *Server) validDashboardToken(tokenStr string) bool {
	if subtle.ConstantTimeCompare([]byte(tokenStr), []byte(s.Service.Config.DashboardToken)) == 1 {
		return true
	}
	token, err := jwt.Parse(tokenStr, func(_ *jwt.Token) (interface{}, error) {
		return jwtKey(s.Service.Config.DashboardToken), nil
	})
	return err == nil && token.Valid
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	apiKey := r.Header.Get("X-API-Key")
	if subtle.ConstantTimeCompare([]byte(apiKey), []byte(s.Service.Config.DashboardToken)) != 1 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
		return
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"sub": "dashboard",
		"iat": now.Unix(),
		"exp": now.Add(24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(jwtKey(s.Service.Config.DashboardToken))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"failed to generate token"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"token":     signed,
		"expiresIn": 86400,
	})
	slog.Info("Dashboard login", "ip", getClientIP(r))
}

func (s *Server) Start() {
	mux := http.NewServeMux()

	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if tokenStr := r.Header.Get("Authorization"); strings.HasPrefix(tokenStr, "Bearer ") {
				token, err := jwt.Parse(strings.TrimPrefix(tokenStr, "Bearer "), func(_ *jwt.Token) (interface{}, error) {
					return jwtKey(s.Service.Config.DashboardToken), nil
				})
				if err == nil && token.Valid {
					next(w, r)
					return
				}
			}

		apiKey := r.Header.Get("X-API-Key")
		if s.validDashboardToken(apiKey) {
			next(w, r)
			return
		}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"Unauthorized"}`))
		}
	}

	mux.HandleFunc("/api/stats", auth(s.handleStats))
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/audit-logs", auth(s.handleAuditLogs))
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		version := "unknown"
		var err error
		if engine, ok := s.Service.TaskManager.Aria2Engine.(*torrent.Aria2Engine); ok && engine.RPC != nil {
			version, err = engine.RPC.GetVersion()
		}

		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"error","component":"aria2","error":"` + err.Error() + `"}`))
			return
		}

		response := fmt.Sprintf(`{"status":"ok","aria2_version":"%s"}`, version)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(response))
	})
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/api/tasks", auth(s.handleTasks))
	mux.HandleFunc("/api/tasks/history", auth(s.handleGetTaskHistory))
	mux.HandleFunc("/api/settings", auth(s.handleSettings))
	mux.HandleFunc("/api/system", auth(s.handleSystem))
	mux.HandleFunc("/api/explorer", auth(s.handleExplorer))
	mux.HandleFunc("/api/explorer/remote", auth(s.handleRemoteExplorer))
	mux.HandleFunc("/api/explorer/remote/link", auth(s.handleRemoteLink))
	mux.HandleFunc("/api/explorer/wipe-orphans", auth(s.handleWipeOrphans))
	mux.HandleFunc("/api/analytics", auth(s.handleAnalytics))
	mux.HandleFunc("/api/logs", auth(s.handleLogs))
	mux.HandleFunc("/api/users", auth(s.handleGetUsers))
	mux.HandleFunc("/api/users/add", auth(s.handleCreateUser))
	mux.HandleFunc("/api/users/update", auth(s.handleUpdateUser))
	mux.HandleFunc("/api/users/delete", auth(s.handleDeleteUser))
	mux.HandleFunc("/api/tools/update", auth(s.handleUpdateTools))
	mux.HandleFunc("/api/config", auth(s.handleConfig))
	mux.HandleFunc("/api/config/reload", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		newCfg := config.Reload()
		s.Service.TaskManager.Mu.Lock()
		s.Service.TaskManager.Config = newCfg
		s.Service.TaskManager.MaxConcurrent = newCfg.MaxConcurrentDownloads
		s.Service.TaskManager.StopDuplicate = newCfg.StopDuplicate
		s.Service.TaskManager.Mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"status":                   "ok",
			"message":                  "Config reloaded successfully",
			"max_concurrent_downloads": newCfg.MaxConcurrentDownloads,
			"stop_duplicate":           newCfg.StopDuplicate,
			"log_level":                newCfg.LogLevel,
		}); err != nil {
			slog.Error("Failed to encode config reload response", "error", err)
		}
	}))
	mux.HandleFunc("/api/explorer/upload", auth(s.handleUpload))
	mux.HandleFunc("/api/explorer/preview", auth(s.handlePreview))

	mux.HandleFunc("/api/ws", s.handleWebsocket)

	mux.HandleFunc("/api/torrent/session", auth(s.handleTorrentSession))
	mux.HandleFunc("/api/torrent/files", auth(s.handleTorrentFiles))
	mux.HandleFunc("/api/torrent/start", auth(s.handleTorrentStart))

	if s.Service.Config.UseWebhook {
		mux.HandleFunc("/api/telegram/webhook", s.handleWebhook)
		slog.Info("Webhook endpoint registered at /api/telegram/webhook")
	}

	mux.HandleFunc("/swagger/", httpSwagger.WrapHandler)

	fs := http.FileServer(http.Dir("./dist"))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api") {
			path := filepath.Join("./dist", filepath.Clean("/"+r.URL.Path))
			_, err := os.Stat(path)

			if os.IsNotExist(err) {
				http.ServeFile(w, r, "./dist/index.html")
				return
			}
		}
		fs.ServeHTTP(w, r)
	}))

	addr := fmt.Sprintf(":%d", s.Port)
	slog.Info("Web Dashboard API starting", "addr", addr)

	allowedOrigin := s.Service.Config.DashboardURL
	if allowedOrigin == "" || allowedOrigin == "127.0.0.1" {
		allowedOrigin = "*"
	}

	s.httpServer = &http.Server{
		Addr:              addr,
		Handler:           securityHeaders(globalMiddleware(mux, allowedOrigin, s.Service.DB)),
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go s.Hub.Run()
	go s.broadcastLoop()

	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			globalRateLimiter.Cleanup()
		}
	}()

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("API Server failed", "error", err)
		}
	}()
}

func (s *Server) Stop(ctx context.Context) error {
	if s.httpServer != nil {
		slog.Info("Shutting down Web Dashboard API Server...")
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

type contextKey string

type apiRateLimiter struct {
	requests map[string]*rateEntry
	limit    int
	window   time.Duration
	mu       sync.Mutex
}

type rateEntry struct {
	resetAt time.Time
	count   int
}

func newAPIRateLimiter(limit int, window time.Duration) *apiRateLimiter {
	return &apiRateLimiter{
		requests: make(map[string]*rateEntry),
		limit:    limit,
		window:   window,
	}
}

func (rl *apiRateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, exists := rl.requests[key]
	if !exists || now.After(entry.resetAt) {
		rl.requests[key] = &rateEntry{count: 1, resetAt: now.Add(rl.window)}
		return true
	}

	if entry.count >= rl.limit {
		return false
	}

	entry.count++
	return true
}

func (rl *apiRateLimiter) count(key string) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	entry, exists := rl.requests[key]
	if !exists || time.Now().After(entry.resetAt) {
		return 0
	}
	return entry.count
}

func (rl *apiRateLimiter) Cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	for k, v := range rl.requests {
		if now.After(v.resetAt) {
			delete(rl.requests, k)
		}
	}
}

var globalRateLimiter = newAPIRateLimiter(60, time.Minute)

var auditExcluded = map[string]bool{
	"/api/health": true, "/api/login": true, "/api/ws": true, "/metrics": true,
}

func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	return r.RemoteAddr
}

func globalMiddleware(next http.Handler, allowedOrigin string, auditRepo repository.AuditRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := allowedOrigin
		if origin == "*" {
			if reqOrigin := r.Header.Get("Origin"); reqOrigin != "" {
				origin = reqOrigin
			}
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if origin != "*" {
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = fmt.Sprintf("req-%d", time.Now().UnixNano())
		}
		w.Header().Set("X-Request-ID", reqID)

		const requestIDKey contextKey = "RequestID"
		ctx := context.WithValue(r.Context(), requestIDKey, reqID)
		r = r.WithContext(ctx)

		if strings.HasPrefix(r.URL.Path, "/api/ws") {
			next.ServeHTTP(w, r)
			return
		}

		clientIP := getClientIP(r)
		rlKey := clientIP
		currentCount := globalRateLimiter.count(rlKey)
		remaining := globalRateLimiter.limit - currentCount
		if remaining < 0 {
			remaining = 0
		}
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(globalRateLimiter.limit))
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(globalRateLimiter.window).Unix(), 10))

		if !globalRateLimiter.Allow(rlKey) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", strconv.Itoa(int(globalRateLimiter.window.Seconds())))
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limit exceeded","retry_after":` + strconv.Itoa(int(globalRateLimiter.window.Seconds())) + `}`))
			return
		}

		if r.Method != http.MethodGet || !auditExcluded[r.URL.Path] {
			actorName := "anonymous"
			if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
				actorName = "dashboard"
			} else if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				actorName = "dashboard(jwt)"
			}
			if auditRepo != nil {
				// #nosec G118 -- fire-and-forget audit log must outlive request lifecycle
				go func(action, actorName, resource, details, ip string) {
					if err := auditRepo.LogAudit(context.Background(), domain.AuditEntry{
						Action:    action,
						ActorName: actorName,
						Resource:  resource,
						Details:   details,
						IPAddress: ip,
					}); err != nil {
						slog.Debug("Failed to log audit entry", "error", err)
					}
				}(r.Method+" "+r.URL.Path, actorName, r.URL.Path, r.URL.RawQuery, clientIP)
			}
		}

		defer func() {
			if panicked := recover(); panicked != nil {
				sentry.CurrentHub().Recover(panicked)
				sentry.Flush(2 * time.Second)
				slog.Error("API panic recovered", "path", r.URL.Path, "panic", panicked)
				http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
			}
		}()

		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(w)
			defer gz.Close()

			gzw := gzipResponseWriter{Writer: gz, ResponseWriter: w}
			next.ServeHTTP(gzw, r)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) broadcastLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		tasks := s.Service.TaskManager.GetActiveTasks()
		metrics.ActiveTasks.Set(float64(len(tasks)))

		var taskSnapshots []interface{}
		for _, t := range tasks {
			taskSnapshots = append(taskSnapshots, t.GetSnapshot())
		}

		v, _ := mem.VirtualMemory()
		c, _ := cpu.Percent(time.Second, false)
		d, _ := disk.Usage("/")
		h, _ := host.Info()
		cpuUsage := 0.0
		if len(c) > 0 {
			cpuUsage = c[0]
		}
		sysInfo := map[string]interface{}{
			"cpu":    cpuUsage,
			"ram":    v.UsedPercent,
			"disk":   d.UsedPercent,
			"uptime": h.Uptime,
		}

		update := map[string]interface{}{
			"type": "update",
			"data": map[string]interface{}{
				"tasks":  taskSnapshots,
				"system": sysInfo,
			},
		}

		payload, err := json.Marshal(update)
		if err == nil {
			s.Hub.Broadcast(payload)
		}
	}
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stats, _ := s.Service.DB.GetBotStats(ctx)
	usersCount, _ := s.Service.DB.GetCount(ctx)
	if stats == nil {
		stats = make(map[string]interface{})
	}
	stats["users_count"] = usersCount
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		slog.Error("Failed to encode stats response", "error", err)
	}
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req struct {
			URL      string `json:"url"`
			Type     string `json:"type"`
			FileName string `json:"fileName"`
			Password string `json:"password"`
			Quality  string `json:"quality"`
			Zip      bool   `json:"zip"`
			Unzip    bool   `json:"unzip"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if req.URL == "" {
			http.Error(w, "URL is required", http.StatusBadRequest)
			return
		}
		if req.Type == "" {
			req.Type = "mirror"
		}

		task, err := s.Service.TaskManager.CreateTask(domain.TaskType(req.Type), req.URL, req.FileName, 0, 0, 0, 0, req.Zip, req.Unzip, req.Password, req.Quality, 0, "", false)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(task.GetSnapshot()); err != nil {
			slog.Error("Failed to encode created task response", "error", err)
		}
		return
	}

	if r.Method == http.MethodDelete {
		taskID := r.URL.Query().Get("id")
		if taskID != "" {
			s.Service.TaskManager.CancelTask(taskID)
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	tasks := s.Service.TaskManager.GetActiveTasks()
	var snapshots []interface{}
	for _, t := range tasks {
		snapshots = append(snapshots, t.GetSnapshot())
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(snapshots); err != nil {
		slog.Error("Failed to encode tasks response", "error", err)
	}
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method == http.MethodPost {
		var settingsUpdate struct {
			DefaultMode        string `json:"DefaultMode"`
			AutoDeleteMessages bool   `json:"AutoDeleteMessages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&settingsUpdate); err != nil {
			http.Error(w, "Invalid settings", http.StatusBadRequest)
			return
		}

		s.Service.Settings.Mu.Lock()
		s.Service.Settings.AutoDeleteMessages = settingsUpdate.AutoDeleteMessages
		s.Service.Settings.DefaultMode = settingsUpdate.DefaultMode
		s.Service.Settings.Mu.Unlock()

		_ = s.Service.SettingsRepo.Set(ctx, "auto_delete_messages", fmt.Sprintf("%v", settingsUpdate.AutoDeleteMessages))
		_ = s.Service.SettingsRepo.Set(ctx, "default_mode", settingsUpdate.DefaultMode)
		w.WriteHeader(http.StatusOK)
		return
	}

	s.Service.Settings.Mu.RLock()
	defer s.Service.Settings.Mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(s.Service.Settings); err != nil {
		slog.Error("Failed to encode settings response", "error", err)
	}
}

func (s *Server) handleSystem(w http.ResponseWriter, _ *http.Request) {
	v, _ := mem.VirtualMemory()
	c, _ := cpu.Percent(time.Second, false)
	d, _ := disk.Usage("/")
	h, _ := host.Info()

	cpuUsage := 0.0
	if len(c) > 0 {
		cpuUsage = c[0]
	}

	sysInfo := map[string]interface{}{
		"cpu":    cpuUsage,
		"ram":    v.UsedPercent,
		"disk":   d.UsedPercent,
		"uptime": h.Uptime,
		"os":     h.OS,
		"arch":   h.KernelArch,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(sysInfo); err != nil {
		slog.Error("Failed to encode system response", "error", err)
	}
}

func (s *Server) handleExplorer(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	fullPath := filepath.Join(s.Service.Config.DownloadDir, path)

	cleanBase, _ := filepath.Abs(s.Service.Config.DownloadDir)
	cleanTarget, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(cleanTarget, cleanBase) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	if r.Method == http.MethodDelete {
		if path == "" || path == "/" || path == "." {
			http.Error(w, "Cannot delete root directory", http.StatusBadRequest)
			return
		}
		// #nosec G703 -- fullPath containment-checked against DownloadDir above
		if err := os.RemoveAll(fullPath); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	files, err := os.ReadDir(fullPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var result []map[string]interface{}
	for _, f := range files {
		info, _ := f.Info()
		name := f.Name()

		if s.shouldSkipFile(path, name) {
			continue
		}

		displayName, status := s.resolveFileStatus(f, name, fullPath)
		if status == "ignore" {
			continue
		}

		result = append(result, map[string]interface{}{
			"name":        name,
			"displayName": displayName,
			"isDir":       f.IsDir(),
			"size":        info.Size(),
			"time":        info.ModTime(),
			"status":      status,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		slog.Error("Failed to encode explorer response", "error", err)
	}
}

func (s *Server) shouldSkipFile(path, name string) bool {
	if path == "" {
		if strings.Contains(name, ":") || len(name) > 40 {
			return true
		}
		if strings.HasSuffix(name, ".binlog") || name == "tqueue" || name == "webhooks" || name == "webhooks_db" {
			return true
		}
	}
	return false
}

func (s *Server) resolveFileStatus(f os.DirEntry, name, fullPath string) (string, string) {
	displayName := name
	status := "folder"
	if !f.IsDir() {
		status = "file"
	}

	if f.IsDir() && (len(name) == 8 || strings.HasPrefix(name, "batch_")) {
		if task := s.Service.TaskManager.GetTask(name); task != nil {
			return task.FileName, "active"
		}

		ctx := context.Background()
		if tr, err := s.Service.DB.GetTaskByID(ctx, name); err == nil && tr != nil {
			return tr.FileName, "finished"
		}

		subPath := filepath.Join(fullPath, name)
		if subFiles, errDir := os.ReadDir(subPath); errDir == nil && len(subFiles) > 0 {
			return subFiles[0].Name(), "orphan"
		}
		return "", "ignore"
	}
	return displayName, status
}

func (s *Server) handleRemoteExplorer(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	remotePath := s.Service.Config.RcloneDest
	if path != "" {
		remotePath = filepath.Join(remotePath, path)
	}
	remotePath = filepath.ToSlash(remotePath)

	configPath := filepath.Join(s.Service.Config.ConfigDir, "rclone.conf")

	if r.Method == http.MethodDelete {
		if path == "" || path == "/" || path == "." {
			http.Error(w, "Cannot delete root remote directory", http.StatusBadRequest)
			return
		}
		// #nosec G702 -- remotePath anchored to configured RcloneDest; rclone runs without shell
		cmd := exec.CommandContext(r.Context(), "rclone", "purge", remotePath, "--config", configPath)
		if purgeErr := cmd.Run(); purgeErr != nil {
			// #nosec G702 -- remotePath anchored to configured RcloneDest; rclone runs without shell
			cmd = exec.CommandContext(r.Context(), "rclone", "deletefile", remotePath, "--config", configPath)
			if deleteErr := cmd.Run(); deleteErr != nil {
				http.Error(w, fmt.Sprintf("Delete failed: %v", deleteErr), http.StatusInternalServerError)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	// #nosec G702 -- remotePath anchored to configured RcloneDest; rclone runs without shell
	cmd := exec.CommandContext(r.Context(), "rclone", "lsjson", remotePath, "--fast-list", "--config", configPath)
	output, err := cmd.Output()
	if err != nil {
		http.Error(w, fmt.Sprintf("Rclone failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	// #nosec G705 -- payload is rclone lsjson JSON served with an explicit application/json header
	_, _ = w.Write(output)
}

func (s *Server) handleRemoteLink(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	remotePath := s.Service.Config.RcloneDest
	if path != "" {
		remotePath = filepath.Join(remotePath, path)
	}
	remotePath = filepath.ToSlash(remotePath)
	configPath := filepath.Join(s.Service.Config.ConfigDir, "rclone.conf")

	// #nosec G702 -- remotePath anchored to configured RcloneDest; rclone runs without shell
	cmd := exec.CommandContext(r.Context(), "rclone", "link", remotePath, "--config", configPath)
	output, err := cmd.Output()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get link: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"link": strings.TrimSpace(string(output))})
}

func (s *Server) handleWipeOrphans(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	baseDir := s.Service.Config.DownloadDir
	files, err := os.ReadDir(baseDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	count := 0
	for _, f := range files {
		name := f.Name()
		if f.IsDir() && (len(name) == 8 || strings.HasPrefix(name, "batch_")) {
			if task := s.Service.TaskManager.GetTask(name); task == nil {
				_ = os.RemoveAll(filepath.Join(baseDir, name))
				count++
			}
		}
	}

	if err := json.NewEncoder(w).Encode(map[string]interface{}{"wiped": count}); err != nil {
		slog.Error("Failed to encode wipe orphans response", "error", err)
	}
}

func (s *Server) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	weekly, _ := s.Service.DB.GetWeeklyStats(ctx)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(weekly); err != nil {
		slog.Error("Failed to encode analytics response", "error", err)
	}
}

func (s *Server) handleLogs(w http.ResponseWriter, _ *http.Request) {
	logPath := filepath.Clean(filepath.Join(s.Service.Config.ConfigDir, "zee-mirror.log"))
	if !strings.HasPrefix(logPath, filepath.Clean(s.Service.Config.ConfigDir)) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		http.Error(w, "Failed to read logs", http.StatusInternalServerError)
		return
	}

	lines := strings.Split(string(data), "\n")
	start := len(lines) - 101
	if start < 0 {
		start = 0
	}
	lastLines := lines[start:]

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"logs": strings.Join(lastLines, "\n"),
	}); err != nil {
		slog.Error("Failed to encode logs response", "error", err)
	}
}

func (s *Server) handleTorrentSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("id")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	s.Service.TaskManager.Mu.RLock()
	session, exists := s.Service.TaskManager.TorrentSessions[sessionID]
	s.Service.TaskManager.Mu.RUnlock()

	if !exists {
		http.Error(w, "Session not found or expired", http.StatusNotFound)
		return
	}

	response := map[string]interface{}{
		"id":       sessionID,
		"url":      session.URL,
		"fileName": session.FileName,
		"zip":      session.Zip,
		"unzip":    session.Unzip,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("Failed to encode torrent session response", "error", err)
	}
}

func (s *Server) handleTorrentFiles(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("id")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	s.Service.TaskManager.Mu.RLock()
	session, exists := s.Service.TaskManager.TorrentSessions[sessionID]
	s.Service.TaskManager.Mu.RUnlock()

	if !exists {
		http.Error(w, "Session not found or expired", http.StatusNotFound)
		return
	}

	if len(session.Files) > 0 {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"files":   session.Files,
			"loading": false,
		}); err != nil {
			slog.Error("Failed to encode torrent files response", "error", err)
		}
		return
	}

	if session.Error != "" {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"files":   []domain.TorrentFile{},
			"loading": false,
			"error":   session.Error,
		}); err != nil {
			slog.Error("Failed to encode torrent error response", "error", err)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"files":    []domain.TorrentFile{},
		"loading":  true,
		"fetching": session.IsFetching,
		"logs":     session.StatusMessages,
		"message":  "Mengambil metadata torrent di background...",
	})
}

func (s *Server) handleTorrentStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request struct {
		SessionID     string `json:"sessionId"`
		SelectedFiles []int  `json:"selectedFiles"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if request.SessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	err := s.Service.StartTorrentWithSelectedFiles(request.SessionID, request.SelectedFiles)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Torrent download started",
	}); err != nil {
		slog.Error("Failed to encode torrent start response", "error", err)
	}
}

func (s *Server) handleGetUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	users, err := s.Service.DB.GetAll(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	type userWithStats struct {
		domain.User
		UsedTasks     int   `json:"usedTasks"`
		UsedBandwidth int64 `json:"usedBandwidth"`
	}

	var result []userWithStats
	for _, u := range users {
		stats, _ := s.Service.DB.GetUserTodayStats(ctx, u.ID)
		result = append(result, userWithStats{
			User:          u,
			UsedTasks:     stats.TotalTasks,
			UsedBandwidth: stats.TotalBandwidth,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		slog.Error("Failed to encode users response", "error", err)
	}
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Role              string `json:"role"`
		ExpiresAt         string `json:"expiresAt"`
		ID                int64  `json:"id"`
		MaxDailyBandwidth int64  `json:"maxDailyBandwidth"`
		MaxDailyTasks     int    `json:"maxDailyTasks"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		if encodeErr := json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"}); encodeErr != nil {
			slog.Debug("Failed to encode error response", "error", encodeErr)
		}
		return
	}

	ctx := r.Context()

	if req.Role != "" {
		if roleErr := s.Service.DB.SetRole(ctx, req.ID, req.Role); roleErr != nil {
			slog.Error("Failed to update role", "error", roleErr)
		}
	}

	if limitsErr := s.Service.DB.SetLimits(ctx, req.ID, req.MaxDailyTasks, req.MaxDailyBandwidth); limitsErr != nil {
		slog.Error("Failed to update limits", "error", limitsErr)
	}

	if req.ExpiresAt != "" {
		if exp, parseErr := time.Parse(time.RFC3339, req.ExpiresAt); parseErr == nil {
			if expErr := s.Service.DB.SetExpiration(ctx, req.ID, exp); expErr != nil {
				slog.Error("Failed to update expiration", "error", expErr)
			}
		} else if exp, parseErr := time.Parse("2006-01-02", req.ExpiresAt); parseErr == nil {
			if expErr := s.Service.DB.SetExpiration(ctx, req.ID, exp); expErr != nil {
				slog.Error("Failed to update expiration", "error", expErr)
			}
		}
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID int64 `json:"id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if s.Service.IsOwner(req.ID) {
		w.WriteHeader(http.StatusForbidden)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "Cannot delete owner"}); err != nil {
			slog.Debug("Failed to encode error response", "error", err)
		}
		return
	}

	ctx := r.Context()
	if dbErr := s.Service.DB.Delete(ctx, req.ID); dbErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		if encErr := json.NewEncoder(w).Encode(map[string]string{"error": dbErr.Error()}); encErr != nil {
			slog.Debug("Failed to encode error response", "error", encErr)
		}
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, readErr := os.ReadFile(".env")
		if readErr != nil {
			data, readErr = os.ReadFile(filepath.Join(".", ".env"))
			if readErr != nil {
				http.Error(w, "Failed to read .env", http.StatusInternalServerError)
				return
			}
		}

		sensitiveKeys := map[string]bool{
			"BOT_TOKEN": true, "OWNER_ID": true, "TELEGRAM_API_HASH": true,
			"TELEGRAM_API_ID": true, "USER_SESSION_STRING": true, "APP_HASH": true,
			"VIKING_USER_HASH": true, "WEB_DASHBOARD_TOKEN": true,
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			parts := strings.SplitN(trimmed, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				if sensitiveKeys[key] {
					lines[i] = key + "=***REDACTED***"
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if encodeErr := json.NewEncoder(w).Encode(map[string]string{"config": strings.Join(lines, "\n")}); encodeErr != nil {
			slog.Error("Failed to encode config response", "error", encodeErr)
		}
	case http.MethodPost:
		var req struct {
			Config string `json:"config"`
		}
		if decodeErr := json.NewDecoder(r.Body).Decode(&req); decodeErr != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		if writeErr := os.WriteFile(".env", []byte(req.Config), 0600); writeErr != nil {
			http.Error(w, "Failed to write .env", http.StatusInternalServerError)
			return
		}

		slog.Info("Configuration updated from dashboard", "by", r.RemoteAddr)
		w.WriteHeader(http.StatusOK)
		if encodeErr := json.NewEncoder(w).Encode(map[string]string{"status": "Configuration updated. Restart may be required for some changes."}); encodeErr != nil {
			slog.Error("Failed to encode config update response", "error", encodeErr)
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseMultipartForm(500 << 20)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	file, handler, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Error retrieving the file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	path := r.FormValue("path")
	path = filepath.Clean(path)
	if strings.Contains(path, "..") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	destDir := filepath.Join(s.Service.Config.DownloadDir, path)
	_ = os.MkdirAll(destDir, 0750)

	cleanBase, _ := filepath.Abs(s.Service.Config.DownloadDir)
	cleanDestDir, _ := filepath.Abs(destDir)
	if !strings.HasPrefix(cleanDestDir, cleanBase) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	safeFilename := filepath.Base(handler.Filename)
	if safeFilename == "." || safeFilename == "/" || safeFilename == ".." {
		http.Error(w, "Invalid filename", http.StatusBadRequest)
		return
	}
	destPath := filepath.Join(destDir, safeFilename)
	dst, err := os.Create(destPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	_, err = io.Copy(dst, file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("File uploaded from dashboard", "name", handler.Filename, "size", handler.Size, "dest", destPath)

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "Upload successful", "file": handler.Filename}); err != nil {
		slog.Error("Failed to encode upload response", "error", err)
	}
}

var previewMimeTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
	".bmp":  "image/bmp",
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".mkv":  "video/x-matroska",
	".pdf":  "application/pdf",
	".txt":  "text/plain; charset=utf-8",
	".log":  "text/plain; charset=utf-8",
	".json": "application/json",
	".csv":  "text/csv; charset=utf-8",
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "Path required", http.StatusBadRequest)
		return
	}

	fullPath := filepath.Join(s.Service.Config.DownloadDir, path)

	cleanBase, _ := filepath.Abs(s.Service.Config.DownloadDir)
	cleanTarget, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(cleanTarget, cleanBase) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// #nosec G703 -- fullPath containment-checked against DownloadDir above
	file, err := os.Open(fullPath)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil || stat.IsDir() {
		http.Error(w, "Invalid file", http.StatusBadRequest)
		return
	}

	ext := strings.ToLower(filepath.Ext(fullPath))
	mimeType, ok := previewMimeTypes[ext]
	if !ok {
		mimeType = "application/octet-stream"
	}

	buf := make([]byte, 512)
	_, _ = file.Read(buf)
	_, _ = file.Seek(0, io.SeekStart)

	detectedMime := http.DetectContentType(buf)
	if detectedMime != "application/octet-stream" {
		mimeType = detectedMime
	}

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), file)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Username          string `json:"username"`
		Role              string `json:"role"`
		ExpiresAt         string `json:"expiresAt"`
		MaxDailyBandwidth int64  `json:"maxDailyBandwidth"`
		ID                int64  `json:"id"`
		MaxDailyTasks     int    `json:"maxDailyTasks"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.ID == 0 {
		http.Error(w, "User ID is required", http.StatusBadRequest)
		return
	}

	user := domain.User{
		ID:                req.ID,
		Username:          req.Username,
		Role:              req.Role,
		MaxDailyTasks:     req.MaxDailyTasks,
		MaxDailyBandwidth: req.MaxDailyBandwidth,
		CreatedAt:         time.Now(),
	}

	if req.ExpiresAt != "" {
		if t, parseErr := time.Parse(time.RFC3339, req.ExpiresAt); parseErr == nil {
			user.ExpiresAt = sql.NullTime{Time: t, Valid: true}
		} else if t, parseErr := time.Parse("2006-01-02", req.ExpiresAt); parseErr == nil {
			user.ExpiresAt = sql.NullTime{Time: t, Valid: true}
		}
	}

	if user.Role == "" {
		user.Role = "authorized"
	}

	ctx := r.Context()
	if upsertErr := s.Service.DB.Upsert(ctx, user); upsertErr != nil {
		http.Error(w, upsertErr.Error(), http.StatusInternalServerError)
		return
	}

	if encodeErr := json.NewEncoder(w).Encode(map[string]string{"status": "success"}); encodeErr != nil {
		slog.Debug("Failed to encode success response", "error", encodeErr)
	}
}

func (s *Server) handleUpdateTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "yt-dlp", "-U")
	output, err := cmd.CombinedOutput()

	result := map[string]interface{}{
		"success": err == nil,
		"output":  string(output),
	}
	if err != nil {
		result["error"] = err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		slog.Error("Failed to encode git status response", "error", err)
	}
}

func (s *Server) handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	entries, err := s.Service.DB.ListAuditLogs(r.Context(), limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entries)
}

func (s *Server) handleGetTaskHistory(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	status := r.URL.Query().Get("status")
	userID, _ := strconv.ParseInt(r.URL.Query().Get("user_id"), 10, 64)

	filter := domain.TaskFilter{
		UserID: userID,
		Limit:  limit,
		Offset: offset,
		Status: status,
	}

	tasks, err := s.Service.DB.ListTasks(r.Context(), filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(tasks); err != nil {
		slog.Error("Failed to encode task history response", "error", err)
	}
}

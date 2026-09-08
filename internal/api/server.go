package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"zee-mirror/internal/metrics"
	"zee-mirror/internal/router"
	"zee-mirror/internal/service"

	_ "zee-mirror/docs" // swagger docs init

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

func (s *Server) Start() {
	mux := http.NewServeMux()

	auth := s.requireAuth

	mux.HandleFunc("/api/stats", auth(s.handleStats))
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/audit-logs", auth(s.handleAuditLogs))
	mux.HandleFunc("/api/health", s.handleHealth)
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
	mux.HandleFunc("/api/config/reload", auth(s.handleConfigReload))
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

package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"zee-mirror/plugins/torrent"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
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

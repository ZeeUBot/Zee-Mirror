package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"zee-mirror/internal/config"
)

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

func (s *Server) handleConfigReload(w http.ResponseWriter, r *http.Request) {
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

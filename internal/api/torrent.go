package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"zee-mirror/internal/domain"
)

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

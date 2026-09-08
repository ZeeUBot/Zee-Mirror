package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"zee-mirror/internal/domain"
)

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

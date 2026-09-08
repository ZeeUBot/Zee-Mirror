package api

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
	"zee-mirror/internal/domain"
)

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

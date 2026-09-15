package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

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
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	return err == nil && token.Valid
}

// authorizeToken accepts dashboard tokens/JWT or an active user's personal API key.
func (s *Server) authorizeToken(ctx context.Context, tokenStr string) bool {
	if tokenStr == "" {
		return false
	}
	if s.validDashboardToken(tokenStr) {
		return true
	}
	u, err := s.Service.DB.GetUserByAPIKey(ctx, tokenStr)
	return err == nil && u != nil && u.IsActive
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if tokenStr := r.Header.Get("Authorization"); strings.HasPrefix(tokenStr, "Bearer ") {
			token, err := jwt.Parse(strings.TrimPrefix(tokenStr, "Bearer "), func(_ *jwt.Token) (interface{}, error) {
				return jwtKey(s.Service.Config.DashboardToken), nil
			}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
			if err == nil && token.Valid {
				next(w, r)
				return
			}
		}

		if s.authorizeToken(r.Context(), r.Header.Get("X-API-Key")) {
			next(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"Unauthorized"}`))
	}
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

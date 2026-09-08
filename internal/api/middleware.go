package api

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"zee-mirror/internal/domain"
	"zee-mirror/internal/repository"

	"github.com/getsentry/sentry-go"
)

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

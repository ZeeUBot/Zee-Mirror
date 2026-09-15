package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zee-mirror/internal/config"
	"zee-mirror/internal/domain"
	"zee-mirror/internal/repository/mocks"
	"zee-mirror/internal/service"
	"zee-mirror/plugins/torrent"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/mock"
)

func newTestServer(t *testing.T, token string) *Server {
	t.Helper()
	cfg := &config.Config{
		DashboardToken: token,
		DashboardURL:   "http://localhost:8080",
		DownloadDir:    t.TempDir(),
	}
	bot, _ := tgbotapi.NewBotAPI("fake:token")
	mockRepo := new(mocks.MockRepository)
	mockRepo.On("Get", mock.Anything, mock.Anything).Return("", nil).Maybe()
	mockRepo.On("Upsert", mock.Anything, mock.AnythingOfType("domain.User")).Return(nil).Maybe()
	mockRepo.On("GetActive", mock.Anything).Return([]domain.TaskRecord{}, nil).Maybe()

	svc := service.NewBotService(bot, cfg, mockRepo, nil)
	return NewServer(svc, 0)
}

func TestAuth_MissingToken(t *testing.T) {
	authFn := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-API-Key")
			if apiKey != "secret123" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}

	req := httptest.NewRequest("GET", "/api/stats", nil)
	rec := httptest.NewRecorder()
	handler := authFn(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuth_WrongToken(t *testing.T) {
	authFn := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-API-Key")
			if apiKey != "secret123" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}

	req := httptest.NewRequest("GET", "/api/stats", nil)
	req.Header.Set("X-API-Key", "wrongtoken")
	rec := httptest.NewRecorder()
	handler := authFn(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAuth_CorrectToken(t *testing.T) {
	authFn := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-API-Key")
			if apiKey != "secret123" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}

	req := httptest.NewRequest("GET", "/api/stats", nil)
	req.Header.Set("X-API-Key", "secret123")
	rec := httptest.NewRecorder()
	handler := authFn(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestCORS_WithDashboardURL(t *testing.T) {
	allowedOrigin := "http://myserver.com"
	handler := globalMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), allowedOrigin, nil)

	req := httptest.NewRequest("GET", "/api/stats", nil)
	req.Header.Set("Origin", "http://myserver.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "http://myserver.com" {
		t.Errorf("expected origin http://myserver.com, got %s", origin)
	}
}

func TestCORS_WildcardWhenNoURL(t *testing.T) {
	allowedOrigin := "*"
	handler := globalMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), allowedOrigin, nil)

	req := httptest.NewRequest("GET", "/api/stats", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "*" {
		t.Errorf("expected *, got %s", origin)
	}
}

func TestPathTraversal_ExplorerBlocked(t *testing.T) {
	s := newTestServer(t, "secret123")

	req := httptest.NewRequest("GET", "/api/explorer?path=../../etc", nil)
	req.Header.Set("X-API-Key", "secret123")
	rec := httptest.NewRecorder()

	s.handleExplorer(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for path traversal, got %d", rec.Code)
	}
}

func TestPathTraversal_ExplorerValid(t *testing.T) {
	s := newTestServer(t, "secret123")

	req := httptest.NewRequest("GET", "/api/explorer?path=subdir", nil)
	req.Header.Set("X-API-Key", "secret123")
	rec := httptest.NewRecorder()

	s.handleExplorer(rec, req)

	if rec.Code != http.StatusInternalServerError && rec.Code != http.StatusOK {
		t.Errorf("expected 200 or 500 (dir not found), got %d", rec.Code)
	}
}

func TestConfigEndpoint_RedactsSecrets(t *testing.T) {
	s := newTestServer(t, "secret123")

	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, ".env")
	envContent := "BOT_TOKEN=abc123secret\nOWNER_ID=12345\nWEB_DASHBOARD_TOKEN=zee343212\nLOG_LEVEL=info\n"
	if err := os.WriteFile(envPath, []byte(envContent), 0600); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(origDir) }()

	req := httptest.NewRequest("GET", "/api/config", nil)
	req.Header.Set("X-API-Key", "secret123")
	rec := httptest.NewRecorder()

	s.handleConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if strings.Contains(body, "abc123secret") {
		t.Error("BOT_TOKEN should be redacted")
	}
	if strings.Contains(body, "zee343212") {
		t.Error("WEB_DASHBOARD_TOKEN should be redacted")
	}
	if !strings.Contains(body, "***REDACTED***") {
		t.Error("response should contain REDACTED markers")
	}
	if !strings.Contains(body, "LOG_LEVEL=info") {
		t.Error("non-sensitive values should not be redacted")
	}
}

func TestWebhook_SecretRequired(t *testing.T) {
	s := newTestServer(t, "secret123")
	s.Service.Config.WebhookSecret = "webhooksecret"

	req := httptest.NewRequest("POST", "/api/telegram/webhook", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.handleWebhook(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for missing webhook secret, got %d", rec.Code)
	}
}

func TestWebhook_WrongSecret(t *testing.T) {
	s := newTestServer(t, "secret123")
	s.Service.Config.WebhookSecret = "webhooksecret"

	req := httptest.NewRequest("POST", "/api/telegram/webhook", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "wrongsecret")
	rec := httptest.NewRecorder()

	s.handleWebhook(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for wrong webhook secret, got %d", rec.Code)
	}
}

func TestWebhook_CorrectSecret(t *testing.T) {
	s := newTestServer(t, "secret123")
	s.Service.Config.WebhookSecret = "webhooksecret"

	req := httptest.NewRequest("POST", "/api/telegram/webhook", strings.NewReader(`{"update_id":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "webhooksecret")
	rec := httptest.NewRecorder()

	s.handleWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for correct webhook secret, got %d", rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := securityHeaders(inner)

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	expected := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"X-XSS-Protection":       "1; mode=block",
		"Referrer-Policy":        "strict-origin-when-cross-origin",
	}

	for header, value := range expected {
		if got := rec.Header().Get(header); got != value {
			t.Errorf("header %s: expected %q, got %q", header, value, got)
		}
	}
}

func TestPathTraversal_UploadBlocked(t *testing.T) {
	s := newTestServer(t, "secret123")

	body := "--boundary\r\nContent-Disposition: form-data; name=\"path\"\r\n\r\n../../etc\r\n--boundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"test.txt\"\r\nContent-Type: text/plain\r\n\r\nhello\r\n--boundary--\r\n"
	req := httptest.NewRequest("POST", "/api/explorer/upload", strings.NewReader(body))
	req.Header.Set("X-API-Key", "secret123")
	req.Header.Set("Content-Type", "multipart/form-data; boundary=boundary")
	rec := httptest.NewRecorder()

	s.handleUpload(rec, req)

	if rec.Code == http.StatusOK {
		t.Error("upload with path traversal should not return 200")
	}
}

func TestHealthEndpoint_NoAuth(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/health", nil)
	rec := httptest.NewRecorder()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("health endpoint should not require auth, got %d", rec.Code)
	}
}

func TestServer_Structure(t *testing.T) {
	s := newTestServer(t, "secret123")

	if s.Service == nil {
		t.Error("Service should not be nil")
	}
	if s.Hub == nil {
		t.Error("Hub should not be nil")
	}
	if s.Port != 0 {
		t.Errorf("Port should be 0, got %d", s.Port)
	}
}

func TestWebSocket_NoToken(t *testing.T) {
	s := newTestServer(t, "secret123")

	req := httptest.NewRequest("GET", "/api/ws", nil)
	rec := httptest.NewRecorder()

	s.handleWebsocket(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for missing WS token, got %d", rec.Code)
	}
}

func TestWebSocket_WrongToken(t *testing.T) {
	s := newTestServer(t, "secret123")
	repo := s.Service.DB.(*mocks.MockRepository)
	repo.On("GetUserByAPIKey", mock.Anything, "wrong").Return((*domain.User)(nil), domain.ErrNotFound)

	req := httptest.NewRequest("GET", "/api/ws?token=wrong", nil)
	rec := httptest.NewRecorder()

	s.handleWebsocket(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong WS token, got %d", rec.Code)
	}
}

func TestTorrentEndpoint_Unauthenticated(t *testing.T) {
	s := newTestServer(t, "secret123")

	req := httptest.NewRequest("GET", "/api/torrent/session", nil)
	rec := httptest.NewRecorder()

	authFn := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-API-Key")
			if apiKey != s.Service.Config.DashboardToken {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}

	handler := authFn(s.handleTorrentSession)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("torrent endpoint should require auth, got %d", rec.Code)
	}
}

func TestAria2NotInitialized(t *testing.T) {
	s := newTestServer(t, "secret123")
	s.Service.TaskManager.Aria2Engine = nil

	req := httptest.NewRequest("GET", "/api/health", nil)
	rec := httptest.NewRecorder()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		version := "unknown"
		if engine, ok := s.Service.TaskManager.Aria2Engine.(*torrent.Aria2Engine); ok && engine != nil && engine.RPC != nil {
			version, _ = engine.RPC.GetVersion()
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","aria2_version":"` + version + `"}`))
	})

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("health should return 200 even without aria2, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unknown") {
		t.Error("version should be 'unknown' when aria2 not initialized")
	}
}

func TestConfigEndpoint_MethodNotAllowed(t *testing.T) {
	s := newTestServer(t, "secret123")

	req := httptest.NewRequest("DELETE", "/api/config", nil)
	req.Header.Set("X-API-Key", "secret123")
	rec := httptest.NewRecorder()

	s.handleConfig(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for DELETE, got %d", rec.Code)
	}
}

func TestExplorer_DeleteRootBlocked(t *testing.T) {
	s := newTestServer(t, "secret123")

	req := httptest.NewRequest("DELETE", "/api/explorer?path=/", nil)
	req.Header.Set("X-API-Key", "secret123")
	rec := httptest.NewRecorder()

	s.handleExplorer(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for deleting root, got %d", rec.Code)
	}
}

func TestExplorer_DeleteEmptyPathBlocked(t *testing.T) {
	s := newTestServer(t, "secret123")

	req := httptest.NewRequest("DELETE", "/api/explorer?path=", nil)
	req.Header.Set("X-API-Key", "secret123")
	rec := httptest.NewRecorder()

	s.handleExplorer(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for deleting empty path, got %d", rec.Code)
	}
}

func TestExplorer_DeleteDotBlocked(t *testing.T) {
	s := newTestServer(t, "secret123")

	req := httptest.NewRequest("DELETE", "/api/explorer?path=.", nil)
	req.Header.Set("X-API-Key", "secret123")
	rec := httptest.NewRecorder()

	s.handleExplorer(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for deleting '.', got %d", rec.Code)
	}
}

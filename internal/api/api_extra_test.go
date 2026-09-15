package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"zee-mirror/internal/domain"
	"zee-mirror/internal/repository/mocks"
)

func TestJWTKey_Deterministic(t *testing.T) {
	assert.Equal(t, jwtKey("abc"), jwtKey("abc"))
	assert.NotEqual(t, jwtKey("abc"), jwtKey("xyz"))
	assert.Len(t, jwtKey("abc"), 32)
}

func TestHandleLogin_MethodNotAllowed(t *testing.T) {
	s := newTestServer(t, "secret123")

	req := httptest.NewRequest("GET", "/api/login", nil)
	rec := httptest.NewRecorder()

	s.handleLogin(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestHandleLogin_WrongKey(t *testing.T) {
	s := newTestServer(t, "secret123")

	req := httptest.NewRequest("POST", "/api/login", nil)
	req.Header.Set("X-API-Key", "wrong")
	rec := httptest.NewRecorder()

	s.handleLogin(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid api key")
}

func TestHandleLogin_Success(t *testing.T) {
	s := newTestServer(t, "secret123")

	req := httptest.NewRequest("POST", "/api/login", nil)
	req.Header.Set("X-API-Key", "secret123")
	rec := httptest.NewRecorder()

	s.handleLogin(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	signed, _ := resp["token"].(string)
	assert.NotEmpty(t, signed)
	assert.InDelta(t, float64(86400), resp["expiresIn"], 0.001)

	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(signed, claims, func(*jwt.Token) (interface{}, error) {
		return jwtKey("secret123"), nil
	})
	assert.NoError(t, err)
	assert.Equal(t, "dashboard", claims["sub"])
}

func TestGetClientIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	assert.Equal(t, "10.0.0.1:1234", getClientIP(req))

	req.Header.Set("X-Real-IP", "10.0.0.2")
	assert.Equal(t, "10.0.0.2", getClientIP(req))

	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	assert.Equal(t, "1.2.3.4", getClientIP(req))

	req.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	assert.Equal(t, "1.2.3.4", getClientIP(req))
}

func TestRateLimiter_AllowCountCleanup(t *testing.T) {
	rl := newAPIRateLimiter(2, 50*time.Millisecond)

	assert.True(t, rl.Allow("ip1"))
	assert.True(t, rl.Allow("ip1"))
	assert.False(t, rl.Allow("ip1")) // limit reached
	assert.True(t, rl.Allow("ip2"))  // independent keys
	assert.Equal(t, 2, rl.count("ip1"))

	time.Sleep(60 * time.Millisecond)
	assert.Equal(t, 0, rl.count("ip1")) // window expired

	rl.Cleanup()
	rl.mu.Lock()
	assert.Empty(t, rl.requests)
	rl.mu.Unlock()
}

func TestShouldSkipFile(t *testing.T) {
	cases := []struct {
		path, name string
		want       bool
	}{
		{"", "video: part1.mkv", true},
		{"", strings.Repeat("x", 41), true},
		{"", "mysql-bin.binlog", true},
		{"", "tqueue", true},
		{"", "webhooks", true},
		{"", "webhooks_db", true},
		{"", "normal.zip", false},
		{"some/path", "weird:name.bin", false}, // checks only apply at root
	}
	for _, c := range cases {
		assert.Equal(t, c.want, (&Server{}).shouldSkipFile(c.path, c.name), "path=%q name=%q", c.path, c.name)
	}
}

func TestResolveFileStatus_BasicEntries(t *testing.T) {
	s := newTestServer(t, "secret123")
	tmp := t.TempDir()

	if err := os.MkdirAll(filepath.Join(tmp, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "a.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil || len(entries) < 2 {
		t.Fatalf("read temp dir: %v", err)
	}

	var dirEntry, fileEntry os.DirEntry
	for _, e := range entries {
		if e.IsDir() {
			dirEntry = e
		} else {
			fileEntry = e
		}
	}
	if dirEntry == nil || fileEntry == nil {
		t.Fatal("expected one dir and one file in temp dir")
	}

	name, status := s.resolveFileStatus(dirEntry, "sub", tmp)
	assert.Equal(t, "sub", name)
	assert.Equal(t, "folder", status)

	name, status = s.resolveFileStatus(fileEntry, "a.txt", tmp)
	assert.Equal(t, "a.txt", name)
	assert.Equal(t, "file", status)
}

func TestResolveFileStatus_FinishedTask(t *testing.T) {
	s := newTestServer(t, "secret123")
	repo := s.Service.DB.(*mocks.MockRepository)
	repo.On("GetTaskByID", mock.Anything, "abcd1234").Return(&domain.TaskRecord{FileName: "done.zip"}, nil)

	tmp := t.TempDir()
	sub := filepath.Join(tmp, "abcd1234")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil || len(entries) == 0 {
		t.Fatalf("read temp dir: %v", err)
	}

	name, status := s.resolveFileStatus(entries[0], "abcd1234", tmp)
	assert.Equal(t, "done.zip", name)
	assert.Equal(t, "finished", status)
}

func TestResolveFileStatus_OrphanDir(t *testing.T) {
	s := newTestServer(t, "secret123")
	repo := s.Service.DB.(*mocks.MockRepository)
	repo.On("GetTaskByID", mock.Anything, "batch_9").Return(nil, nil)

	tmp := t.TempDir()
	sub := filepath.Join(tmp, "batch_9")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "leftover.bin"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil || len(entries) == 0 {
		t.Fatalf("read temp dir: %v", err)
	}

	name, status := s.resolveFileStatus(entries[0], "batch_9", tmp)
	assert.Equal(t, "leftover.bin", name)
	assert.Equal(t, "orphan", status)
}

func TestHandleStats(t *testing.T) {
	s := newTestServer(t, "secret123")
	repo := s.Service.DB.(*mocks.MockRepository)
	repo.On("GetBotStats", mock.Anything).Return(map[string]interface{}{"total_tasks": 3}, nil)
	repo.On("GetCount", mock.Anything).Return(7, nil)

	req := httptest.NewRequest("GET", "/api/stats", nil)
	rec := httptest.NewRecorder()

	s.handleStats(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "total_tasks")
	assert.Contains(t, rec.Body.String(), `"users_count":7`)
}

func TestHandleTasks_BadRequests(t *testing.T) {
	s := newTestServer(t, "secret123")

	req := httptest.NewRequest("POST", "/api/tasks", strings.NewReader("{invalid"))
	rec := httptest.NewRecorder()
	s.handleTasks(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	req = httptest.NewRequest("POST", "/api/tasks", strings.NewReader(`{"url":""}`))
	rec = httptest.NewRecorder()
	s.handleTasks(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "URL is required")
}

func TestRequireAuth_RejectsUnexpectedAlg(t *testing.T) {
	s := newTestServer(t, "secret123")
	handler := s.requireAuth(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	tok := jwt.NewWithClaims(jwt.SigningMethodHS512, jwt.MapClaims{"sub": "dashboard"})
	signed, err := tok.SignedString(jwtKey("secret123"))
	assert.NoError(t, err)

	req := httptest.NewRequest("GET", "/api/stats", nil)
	req.Header.Set("Authorization", "Bearer "+signed)
	rec := httptest.NewRecorder()
	handler(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	req = httptest.NewRequest("GET", "/api/stats", nil)
	req.Header.Set("Authorization", "Bearer "+mustHS256(t, "secret123"))
	rec = httptest.NewRecorder()
	handler(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func mustHS256(t *testing.T, secret string) string {
	t.Helper()
	claims := jwt.MapClaims{"sub": "dashboard", "exp": time.Now().Add(time.Hour).Unix()}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(jwtKey(secret))
	assert.NoError(t, err)
	return signed
}

func TestRequireAuth_PerUserAPIKey(t *testing.T) {
	s := newTestServer(t, "secret123")
	repo := s.Service.DB.(*mocks.MockRepository)
	repo.On("GetUserByAPIKey", mock.Anything, "userkey").Return(&domain.User{ID: 7, IsActive: true}, nil)
	repo.On("GetUserByAPIKey", mock.Anything, "bannedkey").Return(&domain.User{ID: 8, IsActive: false}, nil)
	repo.On("GetUserByAPIKey", mock.Anything, "unknownkey").Return((*domain.User)(nil), domain.ErrNotFound)

	handler := s.requireAuth(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	req := httptest.NewRequest("GET", "/api/stats", nil)
	req.Header.Set("X-API-Key", "userkey")
	rec := httptest.NewRecorder()
	handler(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	req = httptest.NewRequest("GET", "/api/stats", nil)
	req.Header.Set("X-API-Key", "bannedkey")
	rec = httptest.NewRecorder()
	handler(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	req = httptest.NewRequest("GET", "/api/stats", nil)
	req.Header.Set("X-API-Key", "unknownkey")
	rec = httptest.NewRecorder()
	handler(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

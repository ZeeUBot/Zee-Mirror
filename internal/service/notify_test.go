package service

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"zee-mirror/internal/domain"
)

func TestValidNotifyURL(t *testing.T) {
	cases := map[string]bool{
		"":                         true,
		"https://example.com/hook": true,
		"http://localhost:8080/x":  true,
		"ftp://example.com":        false,
		"javascript:alert(1)":      false,
		"https://":                 false,
		"not a url":                false,
	}
	for in, want := range cases {
		if got := ValidNotifyURL(in); got != want {
			t.Errorf("ValidNotifyURL(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestNotifyTaskWebhook(t *testing.T) {
	received := make(chan map[string]interface{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("invalid JSON payload: %v", err)
		}
		received <- payload
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	s := &BotService{}
	s.notifyTaskWebhook(domain.TaskSnapshot{
		ID: "t1", Type: domain.TypeMirror, Status: domain.StatusCompleted,
		FileName: "movie.mkv", RemoteURL: "https://cloud/dl", NotifyURL: srv.URL,
		CompletedAt: time.Now(),
	})

	select {
	case payload := <-received:
		if payload["taskId"] != "t1" || payload["status"] != "completed" || payload["fileName"] != "movie.mkv" {
			t.Errorf("unexpected payload: %+v", payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("webhook POST never arrived")
	}
}

func TestNotifyTaskWebhookSkipsInvalidURL(t *testing.T) {
	s := &BotService{}
	done := make(chan struct{})
	go func() {
		s.notifyTaskWebhook(domain.TaskSnapshot{ID: "t2", NotifyURL: "ftp://bad", Status: domain.StatusFailed})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("invalid URL should return fast, not attempt delivery")
	}
}

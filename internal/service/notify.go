package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"zee-mirror/internal/domain"
)

// ValidNotifyURL rejects non-http(s) callback targets at the trust boundary.
func ValidNotifyURL(raw string) bool {
	if raw == "" {
		return true
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// SetUserNotify points the user's active tasks (memory + DB) at a webhook URL.
// Returns how many in-memory active tasks were updated.
func (s *BotService) SetUserNotify(userID int64, rawURL string) int {
	count := 0
	for _, t := range s.TaskManager.GetActiveTasks() {
		if t.UserID == userID {
			t.Update(func() { t.NotifyURL = rawURL })
			count++
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.TaskManager.DB.SetNotifyURLByUser(ctx, userID, rawURL); err != nil {
		slog.Error("Failed to persist notify url", "error", err, "userID", userID)
	}
	return count
}

// notifyTaskWebhook POSTs a terminal-status payload to the task's notify URL.
// ponytail: user-supplied URL, no SSRF allowlist; add one if untrusted users ever get API keys.
func (s *BotService) notifyTaskWebhook(snap domain.TaskSnapshot) {
	if !ValidNotifyURL(snap.NotifyURL) {
		slog.Warn("Skipping task webhook with invalid notify url", "taskID", snap.ID, "url", snap.NotifyURL)
		return
	}
	payload, err := json.Marshal(map[string]interface{}{
		"taskId":      snap.ID,
		"taskType":    string(snap.Type),
		"status":      string(snap.Status),
		"fileName":    snap.FileName,
		"remoteURL":   snap.RemoteURL,
		"error":       snap.Error,
		"completedAt": snap.CompletedAt,
	})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, snap.NotifyURL, bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("Task webhook delivery failed", "error", err, "taskID", snap.ID)
		return
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		slog.Warn("Task webhook returned non-2xx", "taskID", snap.ID, "status", resp.StatusCode)
	}
}

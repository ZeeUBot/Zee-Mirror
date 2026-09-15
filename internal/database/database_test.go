package database

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"zee-mirror/internal/domain"

	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) (*DB, string) {
	tempDir, err := os.MkdirTemp("", "zee-mirror-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	db, err := NewDB("sqlite", tempDir, "", "../../migrations")
	if err != nil {
		_ = os.RemoveAll(tempDir)
		t.Fatalf("failed to create test db: %v", err)
	}

	return db, tempDir
}

func TestUserOperations(t *testing.T) {
	db, tempDir := setupTestDB(t)
	defer func() { _ = os.RemoveAll(tempDir) }()
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	userID := int64(12345)
	username := "testuser"
	role := "authorized"

	err := db.Upsert(ctx, domain.User{
		ID:                userID,
		Username:          username,
		Role:              role,
		CreatedAt:         time.Now(),
		MaxDailyTasks:     -1,
		MaxDailyBandwidth: -1,
	})
	if err != nil {
		t.Errorf("Upsert failed: %v", err)
	}

	user, err := db.GetByID(ctx, userID)
	if err != nil {
		t.Errorf("GetByID failed: %v", err)
	}
	if user.Username != username || user.Role != role {
		t.Errorf("GetByID got %s, %s; want %s, %s", user.Username, user.Role, username, role)
	}

	newRole := "admin"
	err = db.SetRole(ctx, userID, newRole)
	if err != nil {
		t.Errorf("SetRole failed: %v", err)
	}

	user, _ = db.GetByID(ctx, userID)
	if user.Role != newRole {
		t.Errorf("SetRole failed to update role, got %s want %s", user.Role, newRole)
	}

	count, err := db.GetCount(ctx)
	if err != nil {
		t.Errorf("GetCount failed: %v", err)
	}
	if count != 1 {
		t.Errorf("GetCount got %d, want 1", count)
	}

	users, err := db.GetAll(ctx)
	if err != nil {
		t.Errorf("GetAll failed: %v", err)
	}
	if len(users) != 1 {
		t.Errorf("GetAll got %d users, want 1", len(users))
	}
}

func TestTaskOperations(t *testing.T) {
	db, tempDir := setupTestDB(t)
	defer func() { _ = os.RemoveAll(tempDir) }()
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	task := domain.TaskRecord{
		ID:        "task123",
		GID:       "gid123",
		Type:      "mirror",
		Status:    "downloading",
		URL:       "http://example.com/file.zip",
		FileName:  "file.zip",
		ChatID:    111,
		UserID:    222,
		CreatedAt: time.Now(),
	}

	err := db.Save(ctx, task)
	if err != nil {
		t.Errorf("Save task failed: %v", err)
	}

	gotTask, err := db.GetTaskByID(ctx, task.ID)
	if err != nil {
		t.Errorf("GetTaskByID failed: %v", err)
	}
	if gotTask.ID != task.ID || gotTask.Status != task.Status {
		t.Errorf("GetTaskByID returned wrong task: %+v", gotTask)
	}

	newStatus := "completed"
	err = db.UpdateStatus(ctx, task.ID, newStatus, "")
	if err != nil {
		t.Errorf("UpdateStatus failed: %v", err)
	}

	gotTask, _ = db.GetTaskByID(ctx, task.ID)
	if gotTask.Status != newStatus {
		t.Errorf("UpdateStatus failed to update status, got %s want %s", gotTask.Status, newStatus)
	}

	activeTasks, err := db.GetActive(ctx)
	if err != nil {
		t.Errorf("GetActive failed: %v", err)
	}
	if len(activeTasks) != 0 {
		t.Errorf("GetActive got %d tasks, want 0", len(activeTasks))
	}
}

func TestGetRecoverable(t *testing.T) {
	db, tempDir := setupTestDB(t)
	defer func() { _ = os.RemoveAll(tempDir) }()
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	now := time.Now().UTC()

	tasks := []domain.TaskRecord{
		{ID: "t1", Status: "downloading", CreatedAt: now.Add(-1 * time.Hour), Type: "mirror", ChatID: 1, UserID: 1},
		{ID: "t2", Status: "queued", CreatedAt: now.Add(-2 * time.Hour), Type: "mirror", ChatID: 1, UserID: 1},
		{ID: "t3", Status: "completed", CreatedAt: now.Add(-3 * time.Hour), Type: "mirror", ChatID: 1, UserID: 1},
		{ID: "t4", Status: "downloading", CreatedAt: now.Add(-25 * time.Hour), Type: "mirror", ChatID: 1, UserID: 1},
	}

	for _, task := range tasks {
		_ = db.Save(ctx, task)
	}

	recoverable, err := db.GetRecoverable(ctx)
	if err != nil {
		t.Errorf("GetRecoverable failed: %v", err)
	}

	if len(recoverable) != 2 {
		t.Errorf("GetRecoverable got %d tasks, want 2 (t1 and t2)", len(recoverable))
	}

	foundT1 := false
	foundT2 := false
	for _, rt := range recoverable {
		if rt.ID == "t1" {
			foundT1 = true
		}
		if rt.ID == "t2" {
			foundT2 = true
		}
	}

	if !foundT1 || !foundT2 {
		t.Errorf("GetRecoverable missed t1 or t2")
	}
}

func TestSettingsOperations(t *testing.T) {
	db, tempDir := setupTestDB(t)
	defer func() { _ = os.RemoveAll(tempDir) }()
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	key := "test_key"
	val := "test_val"

	err := db.Set(ctx, key, val)
	if err != nil {
		t.Errorf("Set setting failed: %v", err)
	}

	gotVal, err := db.Get(ctx, key)
	if err != nil {
		t.Errorf("Get setting failed: %v", err)
	}
	if gotVal != val {
		t.Errorf("Get setting got %s, want %s", gotVal, val)
	}
}

func TestUserAPIKey(t *testing.T) {
	db, tempDir := setupTestDB(t)
	defer func() { _ = os.RemoveAll(tempDir) }()
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	userID := int64(777)
	if err := db.Upsert(ctx, domain.User{ID: userID, Username: "keyed", Role: "authorized", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := db.SetAPIKey(ctx, userID, "abc123"); err != nil {
		t.Fatalf("SetAPIKey: %v", err)
	}

	u, err := db.GetUserByAPIKey(ctx, "abc123")
	if err != nil || u == nil || u.ID != userID {
		t.Fatalf("GetUserByAPIKey = (%v, %v), want user %d", u, err, userID)
	}

	if _, gerr := db.GetUserByAPIKey(ctx, "wrong"); !errors.Is(gerr, domain.ErrNotFound) {
		t.Errorf("wrong key: got %v, want ErrNotFound", gerr)
	}
	if _, gerr := db.GetUserByAPIKey(ctx, ""); !errors.Is(gerr, domain.ErrNotFound) {
		t.Errorf("empty key: got %v, want ErrNotFound", gerr)
	}

	users, err := db.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	for _, gu := range users {
		if gu.ID == userID && gu.APIKey != "abc123" {
			t.Errorf("GetAll did not return api_key, got %q", gu.APIKey)
		}
	}
}

func TestSetNotifyURLByUser(t *testing.T) {
	db, tempDir := setupTestDB(t)
	defer func() { _ = os.RemoveAll(tempDir) }()
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	active := domain.TaskRecord{ID: "n1", Status: "downloading", ChatID: 1, UserID: 300}
	done := domain.TaskRecord{ID: "n2", Status: "completed", ChatID: 1, UserID: 300}
	other := domain.TaskRecord{ID: "n3", Status: "downloading", ChatID: 2, UserID: 301}
	for _, tr := range []domain.TaskRecord{active, done, other} {
		if err := db.Save(ctx, tr); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	if err := db.SetNotifyURLByUser(ctx, 300, "https://hook.example/x"); err != nil {
		t.Fatalf("SetNotifyURLByUser: %v", err)
	}

	got, err := db.GetTaskByID(ctx, "n1")
	if err != nil || got.NotifyURL != "https://hook.example/x" {
		t.Errorf("active task notify = (%q, %v), want url", got.NotifyURL, err)
	}
	got, _ = db.GetTaskByID(ctx, "n3")
	if got.NotifyURL != "" {
		t.Errorf("other user task should be untouched, got %q", got.NotifyURL)
	}
	// terminal task n2 not selected, and Save of a record without notify must not wipe it on conflict
	again := domain.TaskRecord{ID: "n1", Status: "downloading", ChatID: 1, UserID: 300}
	_ = db.Save(ctx, again)
	got, _ = db.GetTaskByID(ctx, "n1")
	if got.NotifyURL != "https://hook.example/x" {
		t.Errorf("Save overwrote notify_url: %q", got.NotifyURL)
	}
}

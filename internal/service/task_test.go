package service_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"zee-mirror/internal/domain"
	"zee-mirror/internal/service"

	"github.com/stretchr/testify/assert"
)

func TestTask_GetSnapshot(t *testing.T) {
	task := &service.Task{
		Task: domain.Task{
			ID:         "test-123",
			Type:       service.TypeMirror,
			Status:     service.StatusDownloading,
			URL:        "http://example.com/file.zip",
			FileName:   "file.zip",
			TotalSize:  1024,
			ChatID:     12345,
			UserID:     67890,
			Progress:   45.5,
			Speed:      1024 * 1024,
			Zip:        true,
			Password:   "secret",
			Quality:    "1080p",
			MaxRetries: 3,
			CreatedAt:  time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
			StartedAt:  time.Date(2025, 1, 1, 12, 0, 5, 0, time.UTC),
			RetryCount: 1,
		},
	}

	snapshot := task.GetSnapshot()

	assert.Equal(t, "test-123", snapshot.ID)
	assert.Equal(t, service.TypeMirror, snapshot.Type)
	assert.Equal(t, service.StatusDownloading, snapshot.Status)
	assert.Equal(t, "http://example.com/file.zip", snapshot.URL)
	assert.Equal(t, "file.zip", snapshot.FileName)
	assert.Equal(t, int64(1024), snapshot.TotalSize)
	assert.Equal(t, int64(12345), snapshot.ChatID)
	assert.Equal(t, int64(67890), snapshot.UserID)
	assert.Equal(t, 45.5, snapshot.Progress)
	assert.Equal(t, int64(1024*1024), snapshot.Speed)
	assert.True(t, snapshot.Zip)
	assert.Equal(t, "secret", snapshot.Password)
	assert.Equal(t, "1080p", snapshot.Quality)
	assert.Equal(t, 1, snapshot.RetryCount)
	assert.Equal(t, 3, snapshot.MaxRetries)
}

func TestTask_Update(t *testing.T) {
	task := &service.Task{
		Task: domain.Task{
			ID:     "test-update",
			Status: service.StatusQueued,
		},
	}

	task.Update(func() {
		task.Status = service.StatusDownloading
		task.Progress = 50.0
	})

	assert.Equal(t, service.StatusDownloading, task.Status)
	assert.Equal(t, 50.0, task.Progress)
}

func TestTask_Update_ConcurrentSafety(t *testing.T) {
	task := &service.Task{
		Task: domain.Task{
			ID:       "test-concurrent",
			Progress: 0,
		},
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task.Update(func() {
				task.Progress += 1.0
			})
		}()
	}
	wg.Wait()

	assert.Equal(t, 100.0, task.Progress)
}

func TestTask_Read(t *testing.T) {
	task := &service.Task{
		Task: domain.Task{
			ID:     "test-read",
			Status: service.StatusDownloading,
			URL:    "http://example.com/file.zip",
		},
	}

	var status service.TaskStatus
	var url string
	task.Read(func() {
		status = task.Status
		url = task.URL
	})

	assert.Equal(t, service.StatusDownloading, status)
	assert.Equal(t, "http://example.com/file.zip", url)
}

func TestTask_SetError_CancelWins(t *testing.T) {
	task := &service.Task{Task: domain.Task{ID: "t1", Type: service.TypeMirror, Status: service.StatusDownloading}}
	task.SetStatus(service.StatusCancelled)
	task.SetError("boom")
	assert.Equal(t, service.StatusCancelled, task.Status)
}

func TestBatchTask_Cancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &service.BatchTask{ID: "b1", Status: service.StatusDownloading, Ctx: ctx, CancelFunc: cancel}

	assert.True(t, b.Cancel())
	assert.Equal(t, service.StatusCancelled, b.Status)
	assert.False(t, b.Cancel())
	select {
	case <-ctx.Done():
	default:
		t.Error("batch context should be cancelled")
	}

	b.SetStatus(service.StatusCompleted)
	assert.Equal(t, service.StatusCancelled, b.Status)
}

func TestBatchTask_SetError(t *testing.T) {
	b := &service.BatchTask{ID: "b2", Status: service.StatusDownloading}
	b.SetError("zip failed")
	assert.Equal(t, service.StatusFailed, b.Status)
	assert.Equal(t, "zip failed", b.Error)
	assert.False(t, b.CompletedAt.IsZero())
}

func TestTask_Read_ConcurrentSafety(t *testing.T) {
	task := &service.Task{
		Task: domain.Task{
			ID:       "test-concurrent-read",
			Progress: 50.0,
		},
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var p float64
			task.Read(func() {
				p = task.Progress
			})
			assert.Equal(t, 50.0, p)
		}()
	}
	wg.Wait()
}

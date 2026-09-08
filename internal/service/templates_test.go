package service_test

import (
	"testing"
	"time"

	"zee-mirror/internal/domain"
	"zee-mirror/internal/service"

	"github.com/stretchr/testify/assert"
)

func TestProfessionalMessage(t *testing.T) {
	t.Run("BasicMessage", func(t *testing.T) {
		result := service.ProfessionalMessage("TEST TITLE", "test content")
		assert.Contains(t, result, "TEST TITLE")
		assert.Contains(t, result, "test content")
		assert.Contains(t, result, service.LineSeparator)
	})

	t.Run("WithSpecialCharacters", func(t *testing.T) {
		result := service.ProfessionalMessage("TITLE WITH *STARS*", "content with _underscores_")
		assert.Contains(t, result, "TITLE WITH \\*STARS\\*")
	})
}

func TestHelpDetailMessage(t *testing.T) {
	t.Run("AllFields", func(t *testing.T) {
		result := service.HelpDetailMessage("TEST", "usage text", "how to use", "example code", "extra info")
		assert.Contains(t, result, "TEST")
		assert.Contains(t, result, "usage text")
		assert.Contains(t, result, "how to use")
		assert.Contains(t, result, "example code")
		assert.Contains(t, result, "extra info")
	})

	t.Run("EmptyExtra", func(t *testing.T) {
		result := service.HelpDetailMessage("TEST", "usage", "how to", "example", "")
		assert.Contains(t, result, "TEST")
		assert.Contains(t, result, "usage")
		assert.NotContains(t, result, "extra info")
	})
}

func TestGetStatusHeader(t *testing.T) {
	result := service.GetStatusHeader()
	assert.Contains(t, result, "STATUS TASK AKTIF")
}

func TestFormatTaskProfessional(t *testing.T) {
	t.Run("DownloadingTask", func(t *testing.T) {
		snapshot := domain.TaskSnapshot{
			ID:             "task-123",
			Status:         domain.StatusDownloading,
			FileName:       "video.mp4",
			TotalSize:      1024 * 1024 * 100,
			DownloadedSize: 1024 * 1024 * 50,
			Speed:          1024 * 1024,
			Progress:       50.0,
			ETA:            30 * time.Second,
		}

		result := service.FormatTaskProfessional("en", snapshot)
		assert.Contains(t, result, "task-123")
		assert.Contains(t, result, "video.mp4")
		assert.Contains(t, result, "50")
	})

	t.Run("CompletedTask", func(t *testing.T) {
		snapshot := domain.TaskSnapshot{
			ID:             "task-456",
			Status:         domain.StatusCompleted,
			FileName:       "file.zip",
			TotalSize:      1024 * 1024,
			DownloadedSize: 1024 * 1024,
			Progress:       100.0,
		}

		result := service.FormatTaskProfessional("en", snapshot)
		assert.Contains(t, result, "task-456")
		assert.Contains(t, result, "file.zip")
	})

	t.Run("WithPlaylistInfo", func(t *testing.T) {
		snapshot := domain.TaskSnapshot{
			ID:            "task-789",
			Status:        domain.StatusDownloading,
			FileName:      "video.mp4",
			PlaylistIndex: 3,
			PlaylistCount: 10,
			Progress:      30.0,
		}

		result := service.FormatTaskProfessional("en", snapshot)
		assert.Contains(t, result, "[3/10]")
	})

	t.Run("WithProcessingMessage", func(t *testing.T) {
		snapshot := domain.TaskSnapshot{
			ID:                "task-proc",
			Status:            domain.StatusDownloading,
			FileName:          "video.mp4",
			ProcessingMessage: "Mengkonversi format...",
			Progress:          75.0,
		}

		result := service.FormatTaskProfessional("en", snapshot)
		assert.Contains(t, result, "Mengkonversi format...")
	})

	t.Run("ZeroTotalSize", func(t *testing.T) {
		snapshot := domain.TaskSnapshot{
			ID:       "task-zero",
			Status:   domain.StatusDownloading,
			FileName: "file.zip",
			Progress: 10.0,
		}

		result := service.FormatTaskProfessional("en", snapshot)
		assert.Contains(t, result, "Unknown")
	})
}

func TestGetWelcomeMessage(t *testing.T) {
	result := service.GetWelcomeMessage("en", "TestUser")
	assert.Contains(t, result, "TestUser")
}

func TestGetHelpMainText(t *testing.T) {
	result := service.GetHelpMainText("en")
	assert.NotEmpty(t, result)
}

func TestGetStartKeyboard(t *testing.T) {
	kb := service.GetStartKeyboard("en")
	assert.NotNil(t, kb)
}

func TestGetHelpKeyboard(t *testing.T) {
	kb := service.GetHelpKeyboard("en")
	assert.NotNil(t, kb)
}

func TestLineSeparator(t *testing.T) {
	assert.NotEmpty(t, service.LineSeparator)
	assert.Contains(t, service.LineSeparator, "━")
}

func TestCompactSeparator(t *testing.T) {
	assert.NotEmpty(t, service.CompactSeparator)
	assert.Contains(t, service.CompactSeparator, "─")
}

func TestRepoURL(t *testing.T) {
	assert.Equal(t, "https://github.com/ifauzeee/Zee-Mirror", service.RepoURL)
}

func TestUnknownSize(t *testing.T) {
	assert.Equal(t, "Unknown", service.UnknownSize)
}

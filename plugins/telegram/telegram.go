package telegram

import (
	"context"
	"strings"
	"time"

	"zee-mirror/internal/config"
	"zee-mirror/internal/domain"
	"zee-mirror/internal/downloader"
	"zee-mirror/internal/userbot"
	"zee-mirror/plugins/registry"
)

func init() {
	registry.RegisterDownloadEngine("telegram", func(cfg *config.Config) downloader.DownloadEngine {
		return NewEngine(cfg)
	})
}

type Engine struct {
	Config *config.Config
}

func NewEngine(cfg *config.Config) *Engine {
	return &Engine{Config: cfg}
}

// CanHandle claims private/public Telegram message links.
func (e *Engine) CanHandle(url string) bool {
	return strings.Contains(url, "/c/") || strings.Contains(url, "t.me/c/") || strings.Contains(url, "t.me/")
}

func (e *Engine) Download(_ context.Context, task *domain.Task, outputDir string, onProgress func(downloader.ProgressUpdate)) error {
	ub := userbot.GetInstance(e.Config)
	if err := ub.Start(); err != nil {
		return err
	}

	lastUpdate := time.Now()
	var lastDownloaded int64

	filePath, err := ub.DownloadFile(task.URL, outputDir, func(downloaded, total int64) {
		now := time.Now()
		if now.Sub(lastUpdate) >= time.Second || downloaded == total {
			speed := int64(float64(downloaded-lastDownloaded) / now.Sub(lastUpdate).Seconds())
			progress := float64(0)
			if total > 0 {
				progress = float64(downloaded) / float64(total) * 100
			}

			onProgress(downloader.ProgressUpdate{
				Downloaded: downloaded,
				Total:      total,
				Progress:   progress,
				Speed:      speed,
			})

			lastUpdate = now
			lastDownloaded = downloaded
		}
	})

	if err == nil && filePath != "" {
		task.Mu.Lock()
		task.FileName = filePath
		task.Mu.Unlock()
	}

	return err
}

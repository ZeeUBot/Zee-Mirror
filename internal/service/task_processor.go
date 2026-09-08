package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"zee-mirror/internal/domain"
	"zee-mirror/internal/downloader"
	"zee-mirror/internal/organizer"
	"zee-mirror/internal/queue"
	"zee-mirror/plugins/registry"
)

func (s *BotService) processTask(task *Task) {
	var status TaskStatus
	var url string
	task.Read(func() {
		status = task.Status
		url = task.URL
	})

	if status == StatusCancelled {
		slog.Info("Skipping cancelled task", "taskID", task.ID)
		return
	}

	slog.Info("Starting task processing", "taskID", task.ID, "type", task.Type)

	if s.TaskManager.CheckpointManager != nil {
		go s.TaskManager.CheckpointManager.StartPeriodicCheckpoint(task.Ctx, &task.Task, 30*time.Second)
	}

	if (task.Type == TypeMirror || task.Type == TypeLeech) && strings.HasPrefix(url, "tgfileid://") {
		fileID := strings.TrimPrefix(url, "tgfileid://")
		slog.Info("Resolving Telegram FileID in background", "taskID", task.ID, "fileID", fileID)

		task.SetStatus(StatusFetching)
		s.updateTaskStatus(task)

		tgFile, isOfficial, err := s.GetFileWithFallback(fileID)
		if err != nil {
			task.SetError(fmt.Sprintf("Failed to resolve Telegram file: %v", err))
			s.updateTaskStatus(task)
			return
		}

		var fileURL string
		if filepath.IsAbs(tgFile.FilePath) {
			if _, errStat := os.Stat(tgFile.FilePath); errStat == nil {
				slog.Info("Local TG file detected (Direct path)", "taskID", task.ID, "path", tgFile.FilePath)
				fileURL = "file://" + tgFile.FilePath
			}

			if fileURL == "" {
				translatedPath := strings.Replace(tgFile.FilePath, "/var/lib/telegram-bot-api", s.Config.DownloadDir, 1)
				if _, errStat := os.Stat(translatedPath); errStat == nil {
					slog.Info("Local TG file detected (Translated path)", "taskID", task.ID, "path", translatedPath)
					fileURL = "file://" + translatedPath
				}
			}
		}

		if fileURL == "" {
			fileURL = s.GetFileLink(tgFile, isOfficial)
		}

		task.Update(func() {
			task.URL = fileURL
			if task.TotalSize == 0 {
				task.TotalSize = int64(tgFile.FileSize)
			}
		})

		_ = task.SaveToDB()
		s.updateTaskStatus(task)
		url = fileURL
	}

	if task.Type == TypeMirror || task.Type == TypeLeech {
		if engine, name, ok := registry.SuggestDownloadEngine(url, s.Config); ok {
			slog.Info("Using plugin for link", "taskID", task.ID, "engine", name)
			s.executeDownloadEngine(engine, task)
			return
		}
	}

	switch task.Type {
	case TypeMirror, TypeLeech, TypeTorrent, TypeViking:
		s.executeDownloadEngine(s.TaskManager.Aria2Engine, task)
	case TypeYTDLP, TypeYTDLPLeech:
		s.executeDownloadEngine(s.TaskManager.YTDLPEngine, task)
	case TypeClone:
		s.cloneWithRclone(task)
	}
}

func (s *BotService) executeDownloadEngine(engine downloader.DownloadEngine, task *Task) {
	slog.Info("Starting download engine", "taskID", task.ID, "type", task.Type, "url", task.URL)
	task.SetStatus(StatusDownloading)
	task.Update(func() {
		task.StartedAt = time.Now()
	})
	s.updateTaskStatus(task)

	outputDir := filepath.Join(s.TaskManager.DownloadDir, task.ID)

	if err := os.MkdirAll(outputDir, 0750); err != nil {
		task.SetError(fmt.Sprintf("%v: failed to create output dir: %v", domain.ErrStorage, err))
		s.updateTaskStatus(task)
		return
	}

	if strings.HasPrefix(task.URL, "file://") && task.Type != TypeTorrent {
		s.HandleLocalFileDownload(task, outputDir)
		return
	}

	firstUpdate := true
	lastUpdate := time.Now()
	err := engine.Download(task.Ctx, &task.Task, outputDir, func(up downloader.ProgressUpdate) {
		task.UpdateFromProgressUpdate(up)

		if firstUpdate || time.Since(lastUpdate) >= 3*time.Second {
			s.updateTaskStatus(task)
			lastUpdate = time.Now()
			_ = task.SaveToDB()
			firstUpdate = false
		}
	})

	slog.Info("Download engine returned", "taskID", task.ID, "error", err)

	if err != nil {
		if task.Status == StatusCancelled || errors.Is(err, context.Canceled) {
			slog.Info("Task interrupted or cancelled", "taskID", task.ID)
			s.cleanupTask(task)
			return
		}
		if s.TaskManager.IsShuttingDown() {
			return
		}
		categorizedErr := domain.CategorizeError(err)
		if !domain.IsRetryable(categorizedErr) {
			slog.Info("Non-retryable error, skipping retries", "taskID", task.ID, "error", err)
			task.SetError(categorizedErr.Error())
			s.updateTaskStatus(task)
			s.cleanupTask(task)
			return
		}
		if s.retryTask(task, err.Error()) {
			return
		}
		task.SetError(err.Error())
		s.updateTaskStatus(task)
		s.cleanupTask(task)
		return
	}

	if task.Status == StatusCancelled {
		s.cleanupTask(task)
		return
	}

	s.HandlePostDownload(task, outputDir)
}

func (s *BotService) HandlePostDownload(task *Task, outputDir string) {
	slog.Info("HandlePostDownload called", "taskID", task.ID, "status", task.Status)
	if task.Status == StatusCancelled {
		s.cleanupTask(task)
		return
	}

	task.LocalPath = findDownloadedFile(outputDir, task.Quality)
	slog.Info("findDownloadedFile result", "taskID", task.ID, "localPath", task.LocalPath, "outputDir", outputDir)
	if task.LocalPath == "" {
		if task.Error == "" {
			task.SetError("Downloaded file not found")
		}
		s.updateTaskStatus(task)
		s.cleanupTask(task)
		return
	}
	task.FileName = filepath.Base(task.LocalPath)

	if task.Unzip && (organizer.IsArchiveFile(task.LocalPath) || organizer.LooksLikeArchive(task.LocalPath)) {
		if err := s.extractArchive(task); err != nil {
			task.SetError(fmt.Sprintf("Extraction failed: %v", err))
			s.updateTaskStatus(task)
			s.cleanupTask(task)
			return
		}
	}

	if task.Zip {
		if err := s.createZipArchive(task); err != nil {
			task.SetError(fmt.Sprintf("Compression failed: %v", err))
			s.updateTaskStatus(task)
			s.cleanupTask(task)
			return
		}
	}

	sha256Hex, err := calculateSHA256(task.LocalPath)
	if err != nil {
		slog.Warn("Failed to calculate SHA256", "taskID", task.ID, "path", task.LocalPath, "error", err)
	} else {
		task.Update(func() {
			task.MD5 = sha256Hex
		})
		if updateErr := s.TaskManager.DB.UpdateMD5(context.Background(), task.ID, sha256Hex); updateErr != nil {
			slog.Warn("Failed to persist SHA256", "taskID", task.ID, "error", updateErr)
		}
	}

	switch task.Type {
	case TypeLeech, TypeYTDLPLeech:
		slog.Info("Starting Telegram upload", "taskID", task.ID)
		err = s.UploadToTelegram(task)
	case TypeViking:
		slog.Info("Starting Viking upload", "taskID", task.ID)
		err = s.UploadToViking(task)
	default:
		slog.Info("Starting rclone upload", "taskID", task.ID, "localPath", task.LocalPath)
		err = s.UploadWithRclone(task)
	}

	if err != nil {
		if task.Status == StatusCancelled {
			s.cleanupTask(task)
			return
		}
		task.SetError(fmt.Sprintf("%v: Upload failed: %v", domain.ErrExternal, err))
	} else {
		task.SetStatus(StatusCompleted)
	}
	s.updateTaskStatus(task)
	s.cleanupTask(task)
	s.HandleAutoDelete(task)
}

func (s *BotService) retryTask(task *Task, originalErr string) bool {
	var retries int
	var shouldRetry bool
	task.Update(func() {
		if task.Status != StatusCancelled && task.RetryCount < task.MaxRetries {
			task.RetryCount++
			retries = task.RetryCount
			shouldRetry = true
		}
	})

	if !shouldRetry {
		return false
	}

	backoff := 5 * time.Second
	for i := 1; i < retries; i++ {
		backoff *= 2
		if backoff > 5*time.Minute {
			backoff = 5 * time.Minute
			break
		}
	}

	slog.Info("Retrying task due to error",
		"taskID", task.ID,
		"retry", retries,
		"max", task.MaxRetries,
		"backoff", backoff,
		"error", originalErr)

	go func() {
		select {
		case <-task.Ctx.Done():
			return
		case <-s.TaskManager.ShutdownChan:
			return
		case <-time.After(backoff):
		}

		task.Update(func() {
			task.Status = StatusQueued
			task.Error = ""
		})

		s.updateTaskStatus(task)
		s.TaskManager.Queue.Enqueue(task, queue.PriorityLow)
		select {
		case s.TaskManager.QueueSignal <- struct{}{}:
		default:
		}
	}()

	return true
}

func findDownloadedFile(dir string, quality string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	var candidates []string
	var audioFiles []string
	var videoFiles []string

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		lowerName := strings.ToLower(name)

		if strings.HasSuffix(lowerName, ".aria2") ||
			strings.HasSuffix(lowerName, ".part") ||
			strings.HasSuffix(lowerName, ".ytdl") ||
			strings.HasSuffix(lowerName, ".torrent") ||
			strings.HasSuffix(lowerName, ".temp") {
			continue
		}

		fullPath := filepath.Join(dir, name)
		candidates = append(candidates, fullPath)

		if strings.HasSuffix(lowerName, ".mp3") ||
			strings.HasSuffix(lowerName, ".m4a") ||
			strings.HasSuffix(lowerName, ".ogg") ||
			strings.HasSuffix(lowerName, ".flac") ||
			strings.HasSuffix(lowerName, ".opus") {
			audioFiles = append(audioFiles, fullPath)
		} else if strings.HasSuffix(lowerName, ".mp4") ||
			strings.HasSuffix(lowerName, ".mkv") ||
			strings.HasSuffix(lowerName, ".webm") ||
			strings.HasSuffix(lowerName, ".mov") {
			videoFiles = append(videoFiles, fullPath)
		}
	}

	if quality == "audio" {
		if len(audioFiles) > 0 {
			return getLargest(audioFiles)
		}
	} else if quality != "" {
		if len(videoFiles) > 0 {
			return getLargest(videoFiles)
		}
	}

	if len(candidates) == 1 {
		return candidates[0]
	}

	return getLargest(candidates)
}

func getLargest(paths []string) string {
	var result string
	var maxSize int64
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil {
			if info.Size() > maxSize {
				maxSize = info.Size()
				result = p
			}
		}
	}
	return result
}

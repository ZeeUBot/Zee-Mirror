package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"zee-mirror/internal/domain"
	"zee-mirror/pkg/utils"
)

func (s *BotService) extractArchive(task *Task) error {
	task.SetStatus(StatusExtracting)
	s.updateTaskStatus(task)

	var originalFilename string
	if task.OrigFileName != "" {
		originalFilename = strings.TrimSuffix(task.OrigFileName, filepath.Ext(task.OrigFileName))
	} else {
		originalFilename = strings.TrimSuffix(task.FileName, filepath.Ext(task.FileName))
	}

	extractDir := filepath.Join(filepath.Dir(task.LocalPath), originalFilename)

	if err := os.MkdirAll(extractDir, 0750); err != nil {
		return fmt.Errorf("%w: failed to create extract directory: %v", domain.ErrStorage, err)
	}

	args := []string{
		"x",
		task.LocalPath,
		"-o" + extractDir,
		"-y",
	}

	if task.Password != "" {
		args = append(args, "-p"+task.Password)
	}

	ctx, cancel := context.WithCancel(task.Ctx)
	defer cancel()

	cmd := execCommand(ctx, "7z", args...)

	output, err := cmd.CombinedOutput()

	if task.Status == StatusCancelled {
		return fmt.Errorf("%w", domain.ErrTaskCancelled)
	}

	if err != nil {
		return fmt.Errorf("%w: 7zz extract failed: %v, output: %s", domain.ErrExternal, err, string(output))
	}

	task.LocalPath = extractDir
	task.FileName = filepath.Base(extractDir)

	totalSize, err := utils.CalculateDirSize(extractDir)
	if err != nil {
		slog.Warn("Could not calculate directory size during extract", "error", err, "path", extractDir)
	} else {
		task.Update(func() {
			task.TotalSize = totalSize
			task.DownloadedSize = totalSize
			task.Progress = 100
		})
	}

	return nil
}

func (s *BotService) createZipArchive(task *Task) error {
	task.SetStatus(StatusZipping)
	s.updateTaskStatus(task)

	var zipName string
	if task.OrigFileName != "" {
		zipName = task.OrigFileName + ".zip"
	} else {
		zipName = task.FileName + ".zip"
	}

	zipPath := filepath.Join(filepath.Dir(task.LocalPath), zipName)

	args := []string{
		"a",
		"-tzip",
		zipPath,
		task.LocalPath,
		"-y",
	}

	if task.Password != "" {
		args = append(args, "-p"+task.Password)
		args = append(args, "-mhe=on")
	}

	ctx, cancel := context.WithCancel(task.Ctx)
	defer cancel()

	cmd := execCommand(ctx, "7z", args...)

	output, err := cmd.CombinedOutput()

	if task.Status == StatusCancelled {
		return fmt.Errorf("%w", domain.ErrTaskCancelled)
	}

	if err != nil {
		return fmt.Errorf("%w: 7zz zip failed: %v, output: %s", domain.ErrExternal, err, string(output))
	}

	task.LocalPath = zipPath
	task.FileName = filepath.Base(zipPath)

	return nil
}

func (s *BotService) cleanupTask(task *Task) {
	if s.TaskManager.CheckpointManager != nil {
		if err := s.TaskManager.CheckpointManager.DeleteCheckpoint(task.ID); err != nil {
			slog.Warn("Failed to delete checkpoint", "taskID", task.ID, "error", err)
		}
	}

	taskDir := filepath.Join(s.TaskManager.DownloadDir, task.ID)
	_ = os.RemoveAll(taskDir)
}

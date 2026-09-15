package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path"
	"path/filepath"
	"strings"
	"time"

	"zee-mirror/pkg/utils"
	"zee-mirror/plugins/drive"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (s *BotService) HandleClone(message *tgbotapi.Message, args string) {
	if err := s.CheckQuota(message.From.ID); err != nil {
		s.reply(message, GetErrorMessage("QUOTA EXCEEDED", err.Error()))
		return
	}

	url, _, _, _, _, name, _, _ := utils.ParseFlags(args)
	if url == "" {
		url = utils.ExtractURLFromText(args)
	}

	if url == "" {
		msg := tgbotapi.NewMessage(message.Chat.ID, "❌ *Error*\n\nBerikan URL Google Drive untuk di\\-clone\\.")
		msg.ParseMode = MarkdownV2
		_, _ = s.Bot.Send(msg)
		return
	}

	if !strings.Contains(url, "drive.google.com") && !strings.Contains(url, "docs.google.com") && !strings.Contains(url, "drive.usercontent.google.com") {
		msg := tgbotapi.NewMessage(message.Chat.ID, "❌ *Error*\n\nURL bukan link Google Drive yang valid\\.")
		msg.ParseMode = MarkdownV2
		_, _ = s.Bot.Send(msg)
		return
	}

	replyID := 0
	if message.ReplyToMessage != nil {
		replyID = message.ReplyToMessage.MessageID
	}
	fileName := name
	if fileName == "" {
		fileName = "cloning..."
	}

	task, err := s.TaskManager.CreateTask(TypeClone, url, fileName, message.Chat.ID, message.MessageID, replyID, message.From.ID, false, false, "", "", 0, "", false)
	if err != nil {
		s.HandleCreateTaskError(message.Chat.ID, message.MessageID, err)
		return
	}
	s.HandleAutoDelete(task)
	s.UpdateSharedDashboard(message.Chat.ID, true)
	slog.Info("Clone task created", "taskID", task.ID, "url", url, "name", name)
}

func (s *BotService) cloneWithRclone(task *Task) {
	task.SetStatus(StatusDownloading)
	task.Update(func() {
		task.StartedAt = time.Now()
	})
	s.updateTaskStatus(task)

	driveID, isFolderHint := drive.ExtractDriveID(task.URL)
	if driveID == "" {
		task.SetError("Gagal ekstrak ID dari URL Google Drive")
		s.updateTaskStatus(task)
		return
	}

	configPath := filepath.Join(s.TaskManager.ConfigDir, "rclone.conf")
	remoteName := strings.Split(s.TaskManager.RcloneDest, ":")[0]

	driveName, isDir, driveSize, err := drive.GetDriveInfo(task.Ctx, driveID, configPath, remoteName, isFolderHint, task.URL)
	if err != nil {
		task.SetError(fmt.Sprintf("Gagal mendapatkan info Google Drive: %v", err))
		s.updateTaskStatus(task)
		return
	}

	var name string
	task.Update(func() {
		if (task.FileName == "cloning..." || task.FileName == "") && driveName != "" {
			task.FileName = driveName
		}
		if task.FileName == "cloning..." || task.FileName == "" {
			fallback := utils.GetFileNameFromURL(task.URL)
			if fallback != "" {
				task.FileName = fallback
			} else {
				task.FileName = "file_" + driveID
			}
		}
		name = task.FileName
		if driveSize > 0 {
			task.TotalSize = driveSize
		}
	})
	s.updateTaskStatus(task)

	args, dest := s.buildRcloneCloneArgs(remoteName, driveID, s.TaskManager.RcloneDest, name, configPath, isDir)

	task.RemotePath = dest

	slog.Debug("Running rclone clone command", "taskID", task.ID, "args", strings.Join(args, " "))

	ctx, cancel := context.WithCancel(task.Ctx)
	defer cancel()

	cmd := execCommand(ctx, "rclone", args...)
	stderr, _ := cmd.StderrPipe()

	slog.Info("Starting rclone clone", "taskID", task.ID, "args", strings.Join(args, " "))

	if err := cmd.Start(); err != nil {
		task.SetError(fmt.Sprintf("rclone failed to start: %v", err))
		s.updateTaskStatus(task)
		return
	}

	s.parseCloneProgress(task, stderr)

	if err := cmd.Wait(); err != nil {
		if task.Status == StatusCancelled {
			return
		}
		task.SetError(fmt.Sprintf("rclone clone failed: %v", err))
		s.updateTaskStatus(task)
		return
	}

	if task.Status == StatusCancelled {
		return
	}

	var currentSize int64
	task.Read(func() {
		currentSize = task.TotalSize
	})

	if currentSize <= 0 {
		if finalSize, err := s.getRcloneSize(ctx, dest, configPath); err == nil && finalSize > 0 {
			task.Update(func() {
				task.TotalSize = finalSize
				task.DownloadedSize = finalSize
			})
		} else {
			task.Update(func() {
				if task.DownloadedSize > 0 {
					task.TotalSize = task.DownloadedSize
				}
			})
		}
	}

	s.RcloneUploader.GenerateRcloneLink(ctx, &task.Task, configPath, isDir)
	task.SetStatus(StatusCompleted)
	s.updateTaskStatus(task)
}

func (s *BotService) buildRcloneCloneArgs(remoteName, driveID, rawDest, name, configPath string, isDir bool) ([]string, string) {
	var args []string
	rcloneDest := strings.ReplaceAll(rawDest, "\\", "/")
	dest := path.Join(rcloneDest, name)

	if isDir {
		src := fmt.Sprintf("%s,root_folder_id=%s:", remoteName, driveID)
		args = []string{
			"copy",
			src,
			dest,
			"--config", configPath,
			"--progress",
			"--stats", "1s",
			"--stats-one-line",
			"--transfers", s.Config.RcloneTransfers,
			"--checkers", s.Config.RcloneCheckers,
			"--drive-chunk-size", s.Config.RcloneDriveChunkSize,
			"--buffer-size", s.Config.RcloneBufferSize,
			"--use-mmap",
			"--no-traverse",
			"--drive-pacer-min-sleep", s.Config.RclonePacerMinSleep,
			"--drive-pacer-burst", s.Config.RclonePacerBurst,
			"--drive-server-side-across-configs",
			"--drive-acknowledge-abuse",
			"--drive-description", "Mirrored by Zee-Mirror",
			"--log-level", s.Config.RcloneLogLevel,
		}
	} else {
		args = []string{
			"backend", "copyid",
			fmt.Sprintf("%s:", remoteName),
			driveID,
			dest,
			"--config", configPath,
			"-o", "drive-acknowledge-abuse=true",
			"-o", "drive-description=Mirrored by Zee-Mirror",
		}
	}
	return args, dest
}

func (s *BotService) parseCloneProgress(task *Task, reader io.ReadCloser) {
	scanner := bufio.NewScanner(reader)
	scanner.Split(utils.ScanLinesWithCR)
	lastUpdate := time.Now()

	for scanner.Scan() {
		line := scanner.Text()
		slog.Debug("Rclone clone progress", "taskID", task.ID, "line", line)

		s.handleRcloneLine(task, line)

		if time.Since(lastUpdate) >= 2*time.Second {
			s.updateTaskStatus(task)
			lastUpdate = time.Now()
		}
	}
}

func (s *BotService) getRcloneSize(ctx context.Context, remotePath, configPath string) (int64, error) {
	args := []string{"size", "--json", remotePath, "--config", configPath}
	cmd := execCommand(ctx, "rclone", args...)
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	var res struct {
		TotalBytes int64 `json:"bytes"`
	}
	if err := json.Unmarshal(output, &res); err != nil {
		return 0, err
	}
	return res.TotalBytes, nil
}

func (s *BotService) handleRcloneLine(task *Task, line string) {
	p := drive.ParseRcloneLine(line)

	task.Mu.Lock()
	defer task.Mu.Unlock()

	if p.HasSize {
		task.DownloadedSize = p.Downloaded
		task.TotalSize = p.Total
	}

	if p.HasProgress {
		task.Progress = p.Progress
	}

	if p.HasSpeed {
		task.Speed = p.Speed
	}

	if p.HasETA {
		task.ETA = p.ETA
	}
}

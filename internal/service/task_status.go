package service

import (
	"log/slog"
	"os"
	"strings"
	"zee-mirror/internal/domain"
	"zee-mirror/internal/organizer"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (s *BotService) updateTaskStatus(task *Task) {
	snapshot := task.GetSnapshot()
	if snapshot.NotifyURL != "" && (snapshot.Status == StatusCompleted || snapshot.Status == StatusFailed || snapshot.Status == StatusCancelled) {
		go s.notifyTaskWebhook(snapshot)
	}
	if snapshot.Status != StatusCompleted && snapshot.Status != StatusFailed && snapshot.Status != StatusCancelled {
		s.UpdateSharedDashboardNonBlocking(snapshot.ChatID, false, true)
		return
	}

	s.UpdateSharedDashboard(snapshot.ChatID, false)

	lang := s.GetUserLanguage(snapshot.UserID)
	text := buildTaskStatusText(lang, snapshot, s.Config.IndexURL)

	if snapshot.Status == StatusCompleted && organizer.IsVideoFile(snapshot.FileName) && snapshot.LocalPath != "" {
		var existingID int
		task.Read(func() {
			existingID = task.ResultMessageID
		})

		if existingID == 0 {
			if s.sendVideoWithThumbnail(task, text) {
				return
			}
		}
	}

	slog.Debug("Sending final task message", "task_id", task.ID, "status", snapshot.Status)
	s.sendFinalMessage(task, text)
}

func (s *BotService) sendVideoWithThumbnail(task *Task, text string) bool {
	snapshot := task.GetSnapshot()
	if thumb, err := GenerateThumbnail(task.Ctx, snapshot.LocalPath, s.TaskManager.DownloadDir); err == nil {
		photo := tgbotapi.NewPhoto(snapshot.ChatID, tgbotapi.FilePath(thumb))
		photo.Caption = text
		photo.ParseMode = MarkdownV2
		if keyboard := s.buildCompletionKeyboard(snapshot); keyboard != nil {
			photo.ReplyMarkup = *keyboard
		}
		sentMsg, sendErr := s.Bot.Send(photo)
		if sendErr == nil {
			task.Update(func() {
				task.ResultMessageID = sentMsg.MessageID
			})
			slog.Info("Captured result video message ID", "message_id", sentMsg.MessageID, "task_id", task.ID)
			_ = os.Remove(thumb)
			return true
		}
		slog.Error("Failed to send video with thumbnail", "error", sendErr, "task_id", task.ID)
		_ = os.Remove(thumb)
	}
	return false
}

func (s *BotService) buildCompletionKeyboard(snapshot domain.TaskSnapshot) *tgbotapi.InlineKeyboardMarkup {
	if snapshot.Status != StatusCompleted || snapshot.RemoteURL == "" {
		return nil
	}

	buttons := []tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardButtonURL(domain.BtnTextCloudLink, snapshot.RemoteURL),
	}

	if s.Config.IndexURL != "" {
		remotePath := strings.ReplaceAll(snapshot.RemotePath, "\\", "/")
		rcloneDest := strings.TrimRight(strings.ReplaceAll(s.TaskManager.RcloneDest, "\\", "/"), "/")
		relPath := strings.TrimPrefix(remotePath, rcloneDest)
		if relPath == remotePath {
			parts := strings.SplitN(remotePath, ":", 2)
			if len(parts) > 1 {
				relPath = parts[1]
			}
		}
		indexURL := strings.TrimRight(s.Config.IndexURL, "/") + "/" + strings.TrimLeft(relPath, "/")
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonURL(domain.BtnTextIndexURL, indexURL))
	}

	keyboard := tgbotapi.NewInlineKeyboardMarkup(buttons)
	return &keyboard
}

func (s *BotService) sendFinalMessage(task *Task, text string) {
	snapshot := task.GetSnapshot()

	var msgID int
	task.Read(func() {
		msgID = task.ResultMessageID
	})

	keyboard := s.buildCompletionKeyboard(snapshot)

	if msgID != 0 {
		if keyboard != nil {
			editMarkup := tgbotapi.NewEditMessageReplyMarkup(snapshot.ChatID, msgID, *keyboard)
			if _, err := s.Bot.Send(editMarkup); err != nil {
				slog.Debug("Failed to add keyboard to existing message", "taskID", task.ID, "error", err)
			}
		}

		editCaption := tgbotapi.NewEditMessageCaption(snapshot.ChatID, msgID, text)
		editCaption.ParseMode = MarkdownV2

		if _, err := s.Bot.Send(editCaption); err == nil {
			return
		}

		editText := tgbotapi.NewEditMessageText(snapshot.ChatID, msgID, text)
		editText.ParseMode = MarkdownV2

		if _, err := s.Bot.Send(editText); err == nil {
			return
		}

		slog.Warn("Failed to edit existing message, sending new one", "taskID", task.ID, "msgID", msgID)
	}

	msg := tgbotapi.NewMessage(snapshot.ChatID, text)
	msg.ParseMode = MarkdownV2
	if keyboard != nil {
		msg.ReplyMarkup = *keyboard
	}

	if sentMsg, err := s.Bot.Send(msg); err == nil {
		task.Update(func() {
			task.ResultMessageID = sentMsg.MessageID
		})
		slog.Info("Captured result final message ID", "message_id", sentMsg.MessageID, "task_id", task.ID)
	} else {
		slog.Error("Failed to send final task message", "error", err, "task_id", task.ID, "chatID", snapshot.ChatID)
	}
}

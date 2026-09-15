package file

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
	"zee-mirror/internal/service"
	"zee-mirror/pkg/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func HandleDriveCallback(s *service.BotService, callback *tgbotapi.CallbackQuery, parts []string) {
	if len(parts) < 2 {
		_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, ""))
		return
	}

	action := parts[1]
	switch action {
	case "cd", "c":
		handleCDCallback(s, callback, parts)
	case "home", "h":
		handleHomeCallback(s, callback)
	case "info", "i":
		handleInfoCallback(s, callback, parts)
	case CmdClose, "x":
		handleCloseCallback(s, callback)
	case "confirm_delete", "df":
		handleConfirmDeleteCallback(s, callback, parts)
	case "cancel_delete", "xf":
		handleCancelDeleteCallback(s, callback)
	}
}

func resolveCallbackPath(s *service.BotService, parts []string) string {
	if len(parts) < 3 {
		return ""
	}
	fullPath := strings.Join(parts[2:], ":")
	if strings.HasPrefix(fullPath, "id:") {
		id := strings.TrimPrefix(fullPath, "id:")
		if cached, ok := s.GetPath(id); ok {
			return cached
		}
	}
	return fullPath
}

func handleCDCallback(s *service.BotService, callback *tgbotapi.CallbackQuery, parts []string) {
	fullPath := resolveCallbackPath(s, parts)
	msg := &tgbotapi.Message{
		Chat: callback.Message.Chat,
		From: callback.From,
	}
	HandleDriveList(s, msg, fullPath, callback.Message.MessageID)
	_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "📂 Membuka folder..."))
}

func handleHomeCallback(s *service.BotService, callback *tgbotapi.CallbackQuery) {
	msg := &tgbotapi.Message{
		Chat: callback.Message.Chat,
		From: callback.From,
	}
	HandleDriveList(s, msg, "", callback.Message.MessageID)
	_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "🏠 Kembali ke awal"))
}

func handleInfoCallback(s *service.BotService, callback *tgbotapi.CallbackQuery, parts []string) {
	fullPath := resolveCallbackPath(s, parts)
	handleDriveFileInfoDetailed(s, callback.Message.Chat.ID, callback.Message.MessageID, fullPath)
	_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, ""))
}

func handleCloseCallback(s *service.BotService, callback *tgbotapi.CallbackQuery) {
	deleteMsg := tgbotapi.NewDeleteMessage(callback.Message.Chat.ID, callback.Message.MessageID)
	_, _ = s.Bot.Request(deleteMsg)
	_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "Closed"))
}

func handleConfirmDeleteCallback(s *service.BotService, callback *tgbotapi.CallbackQuery, parts []string) {
	fullPath := resolveCallbackPath(s, parts)
	executeDelete(s, callback, fullPath)
}

func handleCancelDeleteCallback(s *service.BotService, callback *tgbotapi.CallbackQuery) {
	deleteMsg := tgbotapi.NewDeleteMessage(callback.Message.Chat.ID, callback.Message.MessageID)
	_, _ = s.Bot.Request(deleteMsg)
	_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "❌ Cancelled"))
}

func executeDelete(s *service.BotService, callback *tgbotapi.CallbackQuery, fileName string) {
	if fileName == "" || fileName == "/" || fileName == "." {
		_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "❌ Invalid path"))
		return
	}

	targetPath := s.TaskManager.RcloneDest + "/" + fileName
	slog.Info("Attempting to delete drive file", "path", targetPath)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	configPath := s.TaskManager.ConfigDir + "/rclone.conf"

	cmd := exec.CommandContext(ctx, "rclone", "deletefile", targetPath, "--config", configPath)
	_, err := cmd.CombinedOutput()

	if err != nil {
		slog.Warn("deletefile failed, trying purge", "error", err, "path", targetPath)

		cmdPurge := exec.CommandContext(ctx, "rclone", "purge", targetPath, "--config", configPath)
		outputPurge, errPurge := cmdPurge.CombinedOutput()

		if errPurge != nil {
			slog.Error("purge also failed", "error", errPurge, "path", targetPath)
			_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "❌ Failed to delete"))

			errMsg := fmt.Sprintf("❌ *Gagal menghapus file/folder*\n\nTarget: `%s`\nError: `%s`\nOutput: `%s`",
				utils.EscapeMarkdownV2(fileName),
				utils.EscapeMarkdownV2(errPurge.Error()),
				utils.EscapeMarkdownV2(utils.TruncateString(string(outputPurge), 100)))

			s.Reply(callback.Message, errMsg)
			return
		}
		slog.Info("Successfully purged path", "path", targetPath)
	} else {
		slog.Info("Successfully deleted file", "path", targetPath)
	}

	_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "✅ Deleted successfully"))

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID,
		fmt.Sprintf("✅ *File Dihapus*\n\n📁 `%s`", utils.EscapeMarkdownV2(fileName)))
	editMsg.ParseMode = tgbotapi.ModeMarkdownV2

	_, _ = s.Bot.Send(editMsg)
}

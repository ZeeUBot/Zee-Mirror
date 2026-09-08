package basic

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"zee-mirror/internal/service"
	"zee-mirror/pkg/chart"
	"zee-mirror/pkg/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func HandlePing(s *service.BotService, message *tgbotapi.Message) {
	start := time.Now()

	msg := tgbotapi.NewMessage(message.Chat.ID, "🏓 *Pinging\\.\\.\\.*")
	msg.ParseMode = tgbotapi.ModeMarkdownV2
	sentMsg, err := s.Bot.Send(msg)
	if err != nil {
		return
	}

	elapsed := time.Since(start)

	text := fmt.Sprintf("🏓 *Pong\\!* `%v`", elapsed.Round(time.Millisecond))
	editMsg := tgbotapi.NewEditMessageText(message.Chat.ID, sentMsg.MessageID, text)
	editMsg.ParseMode = tgbotapi.ModeMarkdownV2
	_, _ = s.Bot.Send(editMsg)
}

func HandleSpeed(s *service.BotService, message *tgbotapi.Message) {
	msg := tgbotapi.NewMessage(message.Chat.ID, "🚀 *Running Speedtest\\.\\.\\.*")
	msg.ParseMode = tgbotapi.ModeMarkdownV2
	sentMsg, err := s.Bot.Send(msg)
	if err != nil {
		return
	}

	go func() {
		cmd := exec.Command("speedtest-cli", "--simple")
		output, err := cmd.CombinedOutput()
		if err != nil {
			text := fmt.Sprintf("❌ *Speedtest Error*\n\n`%s`", utils.EscapeMarkdownV2Code(err.Error()))
			editMsg := tgbotapi.NewEditMessageText(message.Chat.ID, sentMsg.MessageID, text)
			editMsg.ParseMode = tgbotapi.ModeMarkdownV2
			_, _ = s.Bot.Send(editMsg)
			return
		}

		outStr := string(output)
		lines := strings.Split(strings.TrimSpace(outStr), "\n")
		var result strings.Builder
		result.WriteString("🚀 *Speedtest Result*\n\n")
		for _, line := range lines {
			fmt.Fprintf(&result, "• `%s`\n", utils.EscapeMarkdownV2Code(line))
		}

		dlRe := regexp.MustCompile(`Download:\s+([0-9.]+)\s+Mbit/s`)
		ulRe := regexp.MustCompile(`Upload:\s+([0-9.]+)\s+Mbit/s`)

		var dlVal, ulVal float64
		if match := dlRe.FindStringSubmatch(outStr); len(match) > 1 {
			dlVal, _ = strconv.ParseFloat(match[1], 64)
		}
		if match := ulRe.FindStringSubmatch(outStr); len(match) > 1 {
			ulVal, _ = strconv.ParseFloat(match[1], 64)
		}

		chartBytes, chartErr := chart.GenerateSpeedtestChart(dlVal, ulVal)

		if chartErr == nil && len(chartBytes) > 0 {
			_, _ = s.Bot.Request(tgbotapi.NewDeleteMessage(message.Chat.ID, sentMsg.MessageID))

			msg := tgbotapi.NewPhoto(message.Chat.ID, tgbotapi.FileBytes{Name: "speedtest.png", Bytes: chartBytes})
			msg.Caption = result.String()
			msg.ParseMode = tgbotapi.ModeMarkdownV2
			_, _ = s.Bot.Send(msg)
		} else {
			editMsg := tgbotapi.NewEditMessageText(message.Chat.ID, sentMsg.MessageID, result.String())
			editMsg.ParseMode = tgbotapi.ModeMarkdownV2
			_, _ = s.Bot.Send(editMsg)
		}
	}()
}

func HandleSystem(s *service.BotService, message *tgbotapi.Message) {
	if !s.IsAuthorized(message.From.ID) {
		return
	}

	stats := s.GetSystemStats()
	text := s.FormatSystemStats(stats)

	msg := tgbotapi.NewMessage(message.Chat.ID, text)
	msg.ParseMode = tgbotapi.ModeMarkdownV2
	_, _ = s.Bot.Send(msg)
}

func HandleHealth(s *service.BotService, message *tgbotapi.Message) {
	if !s.IsAuthorized(message.From.ID) {
		return
	}

	checks := s.PerformHealthChecks()
	text := s.FormatHealthCheck(checks)

	msg := tgbotapi.NewMessage(message.Chat.ID, text)
	msg.ParseMode = tgbotapi.ModeMarkdownV2
	_, _ = s.Bot.Send(msg)
}

func HandleLogs(s *service.BotService, message *tgbotapi.Message, _ string) {
	if !s.IsAdmin(message.From.ID) {
		s.Reply(message, service.GetErrorMessage("ACCESS DENIED", "Hanya Admin yang bisa melihat logs\\."))
		return
	}

	logPath := filepath.Join(s.Config.ConfigDir, "zee-mirror.log")

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		s.Reply(message, service.GetErrorMessage("FILE NOT FOUND", "File log tidak ditemukan atau belum dibuat\\."))
		return
	}

	timestamp := time.Now().Format("20060102_150405")
	tempFileName := fmt.Sprintf("zee-mirror_logs_%s.log", timestamp)
	tempPath := filepath.Join(os.TempDir(), tempFileName)

	input, err := os.ReadFile(filepath.Clean(logPath))
	if err != nil {
		s.Reply(message, service.GetErrorMessage("READ ERROR", fmt.Sprintf("Gagal membaca log: %v", err)))
		return
	}

	// #nosec G703 -- temp file name is constants + timestamp under os.TempDir()
	err = os.WriteFile(tempPath, input, 0600)
	if err != nil {
		s.Reply(message, service.GetErrorMessage("WRITE ERROR", fmt.Sprintf("Gagal membuat file temp: %v", err)))
		return
	}
	defer func() { _ = os.Remove(tempPath) }()

	file := tgbotapi.NewDocument(message.Chat.ID, tgbotapi.FilePath(tempPath))
	file.Caption = fmt.Sprintf("📝 *Application Logs:* `%s`", utils.EscapeMarkdownV2(tempFileName))
	file.ParseMode = tgbotapi.ModeMarkdownV2

	_, err = s.Bot.Send(file)
	if err != nil {
		s.Reply(message, service.GetErrorMessage("SEND ERROR", fmt.Sprintf("Gagal mengirim file: %v", err)))
	}
}

func HandleSystemCallback(s *service.BotService, callback *tgbotapi.CallbackQuery, parts []string) {
	if len(parts) < 2 {
		_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, ""))
		return
	}

	action := parts[1]

	switch action {
	case CmdRefresh:
		stats := s.GetSystemStats()
		text := s.FormatSystemStats(stats)

		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, text)
		editMsg.ParseMode = tgbotapi.ModeMarkdownV2
		_, _ = s.Bot.Send(editMsg)
		_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "🔄 Refreshed"))

	case "detailed":
		checks := s.PerformHealthChecks()
		text := s.FormatHealthCheck(checks)

		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, text)
		editMsg.ParseMode = tgbotapi.ModeMarkdownV2
		_, _ = s.Bot.Send(editMsg)
		_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "📊 Detailed view"))

	case "logs":
		msg := &tgbotapi.Message{
			Chat: callback.Message.Chat,
			From: callback.From,
		}
		HandleLogs(s, msg, "")
		_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, ""))

	case "logs_file":
		msg := &tgbotapi.Message{
			Chat: callback.Message.Chat,
			From: callback.From,
		}
		HandleLogs(s, msg, "file")
		_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "Sending file..."))

	case "cleanup":
		if callback.From.ID != s.Config.OwnerID {
			_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "❌ Owner only"))
			return
		}
		go s.PerformDiskCleanup()
		_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "🧹 Cleanup started"))

	case CmdClose:
		deleteMsg := tgbotapi.NewDeleteMessage(callback.Message.Chat.ID, callback.Message.MessageID)
		_, _ = s.Bot.Request(deleteMsg)
		_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "Closed"))
		return
	}
}

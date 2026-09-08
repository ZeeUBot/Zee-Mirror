package api

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"zee-mirror/internal/service"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.Service.Config.WebhookSecret != "" {
		secretHeader := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
		if subtle.ConstantTimeCompare([]byte(secretHeader), []byte(s.Service.Config.WebhookSecret)) != 1 {
			slog.Warn("Webhook request with invalid secret token", "remote", r.RemoteAddr)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("Failed to read webhook body", "error", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var update tgbotapi.Update
	if err := json.Unmarshal(body, &update); err != nil {
		slog.Error("Failed to unmarshal webhook update", "error", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	go s.processWebhookUpdate(update)

	w.WriteHeader(http.StatusOK)
}

func (s *Server) processWebhookUpdate(update tgbotapi.Update) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("PANIC in webhook handler", "recover", r)
		}
	}()
	if s.Router == nil {
		slog.Error("Router not set on API server, cannot process webhook update")
		return
	}

	if update.Message != nil {
		isStart := update.Message.IsCommand() && update.Message.Command() == "start"

		if !s.Service.IsAuthorized(update.Message.From.ID) && !isStart {
			slog.Warn("Unauthorized access attempt (webhook)",
				"userID", update.Message.From.ID,
				"username", update.Message.From.UserName,
				"text", update.Message.Text,
			)

			msg := tgbotapi.NewMessage(update.Message.Chat.ID,
				service.GetErrorMessage("ACCESS DENIED",
					"Anda belum terautentikasi untuk menggunakan bot ini.\nSilakan hubungi Owner untuk mendapatkan akses."))
			msg.ParseMode = tgbotapi.ModeMarkdownV2
			msg.ReplyParameters.MessageID = update.Message.MessageID
			_, _ = s.Service.Bot.Send(msg)
			return
		}
		s.Router.HandleMessage(update.Message)
	} else if update.CallbackQuery != nil {
		data := update.CallbackQuery.Data
		isHelp := strings.HasPrefix(data, "help:")

		if !s.Service.IsAuthorized(update.CallbackQuery.From.ID) && !isHelp {
			slog.Warn("Unauthorized callback attempt (webhook)",
				"userID", update.CallbackQuery.From.ID,
				"username", update.CallbackQuery.From.UserName,
				"data", data,
			)

			cb := tgbotapi.NewCallback(update.CallbackQuery.ID, "🚫 Access Denied")
			_, _ = s.Service.Bot.Request(cb)
			return
		}
		s.Router.HandleCallback(update.CallbackQuery)
	}
}

func (s *Server) SetupWebhook() error {
	webhookURL := s.Service.Config.WebhookURL
	if webhookURL == "" {
		return fmt.Errorf("WEBHOOK_URL is not configured")
	}

	if !strings.HasSuffix(webhookURL, "/") {
		webhookURL += "/"
	}
	webhookURL += "api/telegram/webhook"

	slog.Info("Setting up Telegram webhook", "url", webhookURL)

	webhookConfig, err := tgbotapi.NewWebhook(webhookURL)
	if err != nil {
		return fmt.Errorf("failed to create webhook config: %w", err)
	}

	webhookConfig.MaxConnections = 100

	webhookConfig.AllowedUpdates = []string{"message", "callback_query"}

	if s.Service.Config.WebhookSecret != "" {
		slog.Info("Webhook secret token configured for server-side validation")
	}

	_, err = s.Service.Bot.Request(webhookConfig)
	if err != nil {
		return fmt.Errorf("failed to set webhook: %w", err)
	}

	info, err := s.Service.Bot.GetWebhookInfo()
	if err != nil {
		slog.Warn("Could not verify webhook info", "error", err)
	} else {
		slog.Info("Webhook configured successfully",
			"url", info.URL,
			"pending_updates", info.PendingUpdateCount,
			"max_connections", info.MaxConnections,
			"has_custom_certificate", info.HasCustomCertificate,
		)
		if info.LastErrorDate != 0 {
			slog.Warn("Webhook has recent errors",
				"last_error_date", info.LastErrorDate,
				"last_error_message", info.LastErrorMessage,
			)
		}
	}

	return nil
}

func (s *Server) RemoveWebhook() error {
	slog.Info("Removing Telegram webhook...")

	deleteWebhook := tgbotapi.DeleteWebhookConfig{
		DropPendingUpdates: false,
	}

	_, err := s.Service.Bot.Request(deleteWebhook)
	if err != nil {
		return fmt.Errorf("failed to remove webhook: %w", err)
	}

	slog.Info("Webhook removed successfully")
	return nil
}

package main

import (
	"context"
	"log/slog"
	"strings"
	"zee-mirror/internal/router"
	"zee-mirror/internal/service"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func startPolling(ctx context.Context, bot *tgbotapi.BotAPI, botSvc *service.BotService, r *router.Router) {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	go func() {
		<-ctx.Done()
		slog.Info("Shutting down gracefully (polling mode)...")
		bot.StopReceivingUpdates()
	}()

	processUpdates(ctx, updates, botSvc, r)
}

func processUpdates(ctx context.Context, updates tgbotapi.UpdatesChannel, botSvc *service.BotService, r *router.Router) {
	for {
		select {
		case <-ctx.Done():
			slog.Info("Update processor stopping...")
			return
		case update, ok := <-updates:
			if !ok {
				return
			}
			if update.Message != nil {
				isStart := update.Message.IsCommand() && update.Message.Command() == "start"

				if !botSvc.IsAuthorized(update.Message.From.ID) && !isStart {
					slog.Warn("Unauthorized access attempt", "userID", update.Message.From.ID, "username", update.Message.From.UserName, "text", update.Message.Text)

					msg := tgbotapi.NewMessage(update.Message.Chat.ID, service.GetErrorMessage("ACCESS DENIED", "Anda belum terautentikasi untuk menggunakan bot ini.\nSilakan hubungi Owner untuk mendapatkan akses."))
					msg.ParseMode = tgbotapi.ModeMarkdownV2
					msg.ReplyParameters.MessageID = update.Message.MessageID
					_, _ = botSvc.Bot.Send(msg)
					continue
				}
				go func() {
					defer func() {
						if r := recover(); r != nil {
							slog.Error("PANIC in message handler", "recover", r, "user", update.Message.From.ID, "text", update.Message.Text)
						}
					}()
					r.HandleMessage(update.Message)
				}()
			} else if update.CallbackQuery != nil {
				data := update.CallbackQuery.Data
				isHelp := strings.HasPrefix(data, "help:")

				if !botSvc.IsAuthorized(update.CallbackQuery.From.ID) && !isHelp {
					slog.Warn("Unauthorized callback attempt", "userID", update.CallbackQuery.From.ID, "username", update.CallbackQuery.From.UserName, "data", data)

					cb := tgbotapi.NewCallback(update.CallbackQuery.ID, "🚫 Access Denied")
					_, _ = botSvc.Bot.Request(cb)
					continue
				}
				go func() {
					defer func() {
						if r := recover(); r != nil {
							slog.Error("PANIC in callback handler", "recover", r, "user", update.CallbackQuery.From.ID, "data", data)
						}
					}()
					r.HandleCallback(update.CallbackQuery)
				}()
			}
		}
	}
}

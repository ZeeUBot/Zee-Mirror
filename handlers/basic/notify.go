package basic

import (
	"fmt"
	"strings"

	"zee-mirror/internal/service"
	"zee-mirror/pkg/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// HandleNotify registers (or clears with "off") a webhook URL that gets a POST
// whenever one of the user's tasks reaches a terminal state.
func HandleNotify(s *service.BotService, m *tgbotapi.Message, args string) {
	if m.From == nil {
		return
	}
	arg := strings.TrimSpace(args)
	if arg == "" {
		notifyReply(s, m, "🔔 NOTIFY WEBHOOK\n\nGunakan: /notify <webhook_url>\nHapus: /notify off\n\nWebhook akan menerima POST JSON saat task kamu selesai/gagal.")
		return
	}

	if strings.EqualFold(arg, "off") {
		n := s.SetUserNotify(m.From.ID, "")
		notifyReply(s, m, fmt.Sprintf("🔕 Webhook notifikasi dinonaktifkan (%d task aktif)", n))
		return
	}

	if !service.ValidNotifyURL(arg) {
		notifyReply(s, m, "❌ URL tidak valid — harus http/https")
		return
	}

	n := s.SetUserNotify(m.From.ID, arg)
	text := fmt.Sprintf("✅ Webhook disimpan untuk %d task aktif\n\nURL: `%s`\nWebhook akan menerima POST JSON saat task selesai/gagal\\.",
		n, utils.EscapeMarkdownV2Code(arg))
	detail := tgbotapi.NewMessage(m.Chat.ID, text)
	detail.ParseMode = tgbotapi.ModeMarkdownV2
	_, _ = s.Bot.Send(detail)
}

func notifyReply(s *service.BotService, m *tgbotapi.Message, text string) {
	_, _ = s.Bot.Send(tgbotapi.NewMessage(m.Chat.ID, text))
}

package basic

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"zee-mirror/internal/service"
	"zee-mirror/pkg/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func HandleDashboardCallback(s *service.BotService, callback *tgbotapi.CallbackQuery) {
	parts := strings.Split(callback.Data, ":")
	if len(parts) < 2 {
		return
	}
	action := parts[1]

	if handleDashboardAction(s, callback, action, parts) {
		return
	}

	text := getDashboardModeText(action)
	keyboard := tgbotapi.InlineKeyboardMarkup{}

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, text)
	editMsg.ParseMode = tgbotapi.ModeMarkdownV2
	editMsg.ReplyMarkup = &keyboard
	_, _ = s.Bot.Send(editMsg)
	_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, ""))
}

func handleDashboardAction(s *service.BotService, callback *tgbotapi.CallbackQuery, action string, parts []string) bool {
	switch action {
	case "page":
		handlePagination(s, callback, parts)
	case "status":
		handleStatusFromCallback(s, callback)
	case "ping":
		handlePingFromCallback(s, callback)
	case "speed":
		handleSpeedFromCallback(s, callback)
	case "stats":
		handleStatsFromCallback(s, callback)
	case "storage":
		handleStoragesFromCallback(s, callback)
	case "files":
		handleDriveListFromCallback(s, callback)
	case "media":
		sendMediaMenu(s, callback)
	case "system":
		handleSystemFromCallback(s, callback)
	case "back":
		lang := s.GetUserLanguage(callback.From.ID)
		content := service.GetWelcomeMessage(lang, callback.From.FirstName)
		kb := service.GetStartKeyboard(lang)
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, content)
		editMsg.ParseMode = tgbotapi.ModeMarkdownV2
		if len(kb.InlineKeyboard) > 0 {
			editMsg.ReplyMarkup = &kb
		}
		_, _ = s.Bot.Send(editMsg)
		_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, ""))
	case "close":
		deleteMsg := tgbotapi.NewDeleteMessage(callback.Message.Chat.ID, callback.Message.MessageID)
		_, _ = s.Bot.Request(deleteMsg)
		_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "Closed"))
	default:
		return false
	}
	return true
}

func handlePagination(s *service.BotService, callback *tgbotapi.CallbackQuery, parts []string) {
	if len(parts) >= 3 {
		page, _ := strconv.Atoi(parts[2])
		s.TaskManager.Mu.Lock()
		s.TaskManager.StatusPages[callback.Message.Chat.ID] = page
		s.TaskManager.Mu.Unlock()
		s.UpdateSharedDashboard(callback.Message.Chat.ID, false)
		_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, fmt.Sprintf("Halaman %d", page+1)))
	}
}

func handleStatusFromCallback(s *service.BotService, callback *tgbotapi.CallbackQuery) {
	lang := s.GetUserLanguage(callback.From.ID)
	tasks := s.TaskManager.GetActiveTasks()

	if len(tasks) == 0 {
		content := service.GetErrorMessage("STATUS", "Tidak ada task aktif.")
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, content)
		editMsg.ParseMode = tgbotapi.ModeMarkdownV2
		_, _ = s.Bot.Send(editMsg)
		_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "📭 Empty"))
		return
	}

	text := service.GetStatusHeader()
	for _, task := range tasks {
		snapshot := task.GetSnapshot()
		text += service.FormatTaskProfessional(lang, snapshot) + "\n"
	}

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, text)
	editMsg.ParseMode = tgbotapi.ModeHTML
	_, _ = s.Bot.Send(editMsg)
	_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "🔄 Updated"))
}

func handlePingFromCallback(s *service.BotService, callback *tgbotapi.CallbackQuery) {
	start := time.Now()
	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, "🏓 *Pinging\\.\\.\\.*")
	editMsg.ParseMode = tgbotapi.ModeMarkdownV2
	_, _ = s.Bot.Send(editMsg)

	elapsed := time.Since(start)
	text := fmt.Sprintf("🏓 *Pong\\!* `%v`", elapsed.Round(time.Millisecond))

	finalEdit := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, text)
	finalEdit.ParseMode = tgbotapi.ModeMarkdownV2
	_, _ = s.Bot.Send(finalEdit)
	_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "🏓 Pong!"))
}

func handleSpeedFromCallback(s *service.BotService, callback *tgbotapi.CallbackQuery) {
	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, "🚀 *Running Speedtest\\.\\.\\.*")
	editMsg.ParseMode = tgbotapi.ModeMarkdownV2
	_, _ = s.Bot.Send(editMsg)

	go func() {
		cmd := execCommand(context.Background(), "speedtest-cli", "--simple")
		output, err := cmd.CombinedOutput()

		var text string
		if err != nil {
			text = fmt.Sprintf("❌ *Speedtest Error*\n\n`%s`", utils.EscapeMarkdownV2(err.Error()))
		} else {
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			var result strings.Builder
			result.WriteString("🚀 *Speedtest Result*\n\n")
			for _, line := range lines {
				fmt.Fprintf(&result, "• `%s`\n", utils.EscapeMarkdownV2(line))
			}
			text = result.String()
		}

		finalEdit := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, text)
		finalEdit.ParseMode = tgbotapi.ModeMarkdownV2
		_, _ = s.Bot.Send(finalEdit)
	}()
	_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "🚀 Testing speed..."))
}

func handleStatsFromCallback(s *service.BotService, callback *tgbotapi.CallbackQuery) {
	ctx := context.Background()
	stats, err := s.DB.GetBotStats(ctx)
	if err != nil {
		_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "❌ Error"))
		return
	}

	userStats, _ := s.DB.GetUserStats(ctx, callback.From.ID)
	dailyStats, _ := s.DB.GetTodayStats(ctx)
	userDailyStats, _ := s.DB.GetUserTodayStats(ctx, callback.From.ID)

	text := FormatStatsMessage(stats, userStats, dailyStats, userDailyStats)

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, text)
	editMsg.ParseMode = tgbotapi.ModeMarkdownV2
	_, _ = s.Bot.Send(editMsg)
	_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "📊 Statistics"))
}

func handleStoragesFromCallback(s *service.BotService, callback *tgbotapi.CallbackQuery) {
	providers, err := s.GetAvailableStorages()
	if err != nil {
		_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "❌ Error"))
		return
	}

	var text strings.Builder
	text.WriteString("💾 *Available Storage Providers*\n")
	text.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	for _, p := range providers {
		fmt.Fprintf(&text, "%s *%s* \\(%s\\)\n", p.Icon, utils.EscapeMarkdownV2(p.Name), utils.EscapeMarkdownV2(p.Type))
	}

	text.WriteString("\n━━━━━━━━━━━━━━━━━━━━━━━━\n")
	text.WriteString("*Current:* `" + utils.EscapeMarkdownV2(s.TaskManager.RcloneDest) + "`\n\n")

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, text.String())
	editMsg.ParseMode = tgbotapi.ModeMarkdownV2
	_, _ = s.Bot.Send(editMsg)
	_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "💾 Storages"))
}

func handleSystemFromCallback(s *service.BotService, callback *tgbotapi.CallbackQuery) {
	stats := s.GetSystemStats()
	text := s.FormatSystemStats(stats)

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, text)
	editMsg.ParseMode = tgbotapi.ModeMarkdownV2
	_, _ = s.Bot.Send(editMsg)
	_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "🖥️ System"))
}

func handleDriveListFromCallback(s *service.BotService, callback *tgbotapi.CallbackQuery) {
	basePath := strings.TrimSuffix(s.TaskManager.RcloneDest, "/")
	path := basePath

	loadingMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, "🔍 *Memuat daftar file\\.\\.\\.*")
	loadingMsg.ParseMode = tgbotapi.ModeMarkdownV2
	_, _ = s.Bot.Send(loadingMsg)

	files, err := s.ListDriveFiles(path)
	if err != nil {
		text := fmt.Sprintf("❌ *Gagal memuat daftar file*\n\nError: %s", utils.EscapeMarkdownV2(err.Error()))
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, text)
		editMsg.ParseMode = tgbotapi.ModeMarkdownV2
		_, _ = s.Bot.Send(editMsg)
		return
	}

	text := s.FormatDriveFileList("/", files)

	finalEdit := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, text)
	finalEdit.ParseMode = tgbotapi.ModeMarkdownV2
	_, _ = s.Bot.Send(finalEdit)
	_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "📁 Drive List"))
}

func sendMediaMenu(s *service.BotService, callback *tgbotapi.CallbackQuery) {
	content := "Operasi media untuk video/audio:\n\n" +
		"• `/extractaudio` ─ Ambil audio dari video\n" +
		"• `/compress` ─ Kompres ukuran video\n" +
		"• `/thumbnail` ─ Generate thumbnail\n" +
		"• `/screenshots` ─ Multi screenshot\n" +
		"• `/subtitle` ─ Soft\\-sub \\(Embed subtitle\\)\n" +
		"• `/hardsub` ─ Hard\\-sub \\(Burn subtitle\\)\n" +
		"• `/rescale` ─ Ubah resolusi video\n" +
		"• `/convert` ─ Konversi format file\n" +
		"• `/mediainfo` ─ Info detail file\n\n" +
		"💡 *Cara pakai:* Reply ke file video dengan command"

	text := service.ProfessionalMessage("MEDIA PROCESSING", content)

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, text)
	editMsg.ParseMode = tgbotapi.ModeMarkdownV2
	_, _ = s.Bot.Send(editMsg)
	_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "🎵 Media Menu"))
}

func getDashboardModeText(action string) string {
	switch action {
	case "mirror":
		return service.ProfessionalMessage("MIRROR MODE",
			"Upload file langsung ke Google Drive\\.\n\n"+
				"📌 *CARA PAKAI*\n"+
				"• Reply ke file dengan `/mirror`\n"+
				"• Atau kirim: `/mirror URL`\n\n"+
				"🏷️ *FLAGS*\n"+
				"• `\\-z` ─ Zip sebelum upload\n"+
				"• `\\-uz` ─ Unzip setelah download\n"+
				"• `\\-p PASS` ─ Password zip")

	case "leech":
		return service.ProfessionalMessage("LEECH MODE",
			"Download file dari URL ke server\\.\n\n"+
				"✅ *SUPPORT*\n"+
				"• HTTP/HTTPS • FTP • Magnet\n\n"+
				"📌 *CARA PAKAI*\n"+
				"• `/leech` \\<URL\\>")

	case "ytdlp":
		return service.ProfessionalMessage("YT-DLP MODE",
			"Download video dari 1000\\+ situs\\.\n\n"+
				"✅ *SUPPORT*\n"+
				"• YouTube • Twitter • TikTok • etc\\.\n\n"+
				"📌 *CARA PAKAI*\n"+
				"• `/ytdlp` \\<URL\\>")

	case "torrent":
		return service.ProfessionalMessage("TORRENT MODE",
			"Download via magnet atau file torrent\\.\n\n"+
				"📌 *CARA PAKAI*\n"+
				"• `/torrent` \\<magnet\\_link\\>\n"+
				"• Reply ke file `.torrent` dengan `/torrent`")

	case "clone":
		return service.ProfessionalMessage("CLONE MODE",
			"Clone file/folder Google Drive\\.\n\n"+
				"📌 *CARA PAKAI*\n"+
				"• `/clone` \\<GDRIVE\\_URL\\>")

	case "batch":
		return service.ProfessionalMessage("BATCH MODE",
			"Download multiple URLs sekaligus\\.\n\n"+
				"📌 *CARA PAKAI*\n"+
				"`/batch`\n"+
				"`URL1`\n"+
				"`URL2`")

	case "search":
		return service.ProfessionalMessage("SEARCH MODE",
			"Cari torrent dari berbagai sumber\\.\n\n"+
				"📌 *CARA PAKAI*\n"+
				"• `/search` \\<keyword\\>")

	case "hardsub":
		return service.ProfessionalMessage("HARD-SUB MODE",
			"Burn subtitle permanen ke dalam video\\.\n\n"+
				"📌 *CARA PAKAI*\n"+
				"• Reply ke video dengan `/hardsub subtitle.srt`\n"+
				"• Atau: `/hardsub video.mp4 subtitle.srt`\n\n"+
				"🔥 *INFO*\n"+
				"Proses ini memerlukan re-encoding video, jadi akan memakan waktu lebih lama dibanding soft-sub.")

	case "rescale":
		return service.ProfessionalMessage("RESCALE MODE",
			"Ubah resolusi video (Transcoding)\\.\n\n"+
				"📌 *CARA PAKAI*\n"+
				"• Reply ke video dengan `/rescale 720p`\n"+
				"• Atau: `/rescale video.mp4 1280x720`\n\n"+
				"🏷️ *PRESET*\n"+
				"`4k`, `2k`, `1080p`, `720p`, `480p`, `360p` atau format custom `WxH`.")

	case "back":
		return ""

	default:
		return service.GetErrorMessage("UNKNOWN", "Aksi tidak dikenal.")
	}
}

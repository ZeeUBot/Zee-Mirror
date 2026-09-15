package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"zee-mirror/internal/service"
	"zee-mirror/pkg/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Provider struct {
	Name       string
	RemoteName string
	Type       string
	Icon       string
}

func GetAvailableStorages(s *service.BotService) ([]Provider, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	configPath := s.TaskManager.ConfigDir + "/rclone.conf"

	cmd := execCommand(ctx, "rclone", "listremotes", "--config", configPath, "--long")
	output, err := cmd.Output()

	if err != nil {
		return nil, fmt.Errorf("failed to list remotes: %v", err)
	}

	var providers []Provider
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			name := strings.TrimSuffix(parts[0], ":")
			remoteType := parts[1]

			provider := Provider{
				Name:       name,
				RemoteName: parts[0],
				Type:       remoteType,
				Icon:       getStorageIcon(remoteType),
			}
			providers = append(providers, provider)
		}
	}

	return providers, nil
}

func getStorageIcon(storageType string) string {
	switch strings.ToLower(storageType) {
	case "drive":
		return "📁"
	case "onedrive":
		return "☁️"
	case "dropbox":
		return "📦"
	case "mega":
		return "🔷"
	case "s3", "b2":
		return "🪣"
	case "ftp", "sftp":
		return "🖥️"
	case "webdav":
		return "🌐"
	default:
		return "💾"
	}
}

func HandleStorages(s *service.BotService, message *tgbotapi.Message) {
	if !s.IsAuthorized(message.From.ID) {
		return
	}

	providers, err := GetAvailableStorages(s)
	if err != nil {
		s.Reply(message, fmt.Sprintf("❌ *Gagal mendapatkan daftar storage*\n\n%s", utils.EscapeMarkdownV2(err.Error())))
		return
	}

	if len(providers) == 0 {
		s.Reply(message, "📭 *Tidak ada storage yang dikonfigurasi*\n\nTambahkan remote di rclone\\.conf")
		return
	}

	var text strings.Builder
	text.WriteString("💾 *Available Storage Providers*\n")
	text.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	for _, p := range providers {
		fmt.Fprintf(&text, "%s *%s* \\(%s\\)\n", p.Icon, utils.EscapeMarkdownV2(p.Name), utils.EscapeMarkdownV2(p.Type))
	}

	text.WriteString("\n━━━━━━━━━━━━━━━━━━━━━━━━\n")
	text.WriteString("*Current:* `" + utils.EscapeMarkdownV2Code(s.TaskManager.RcloneDest) + "`\n\n")
	text.WriteString("Pilih storage untuk upload\\:")

	msg := tgbotapi.NewMessage(message.Chat.ID, text.String())
	msg.ParseMode = tgbotapi.ModeMarkdownV2
	_, _ = s.Bot.Send(msg)
}

func HandleStorageCallback(s *service.BotService, callback *tgbotapi.CallbackQuery, parts []string) {
	if len(parts) < 2 {
		_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, ""))
		return
	}

	action := parts[1]
	storageName := ""
	if len(parts) > 2 {
		storageName = parts[2]
	}

	switch action {
	case "select":
		handleStorageSelect(s, callback, storageName)
	case "browse":
		handleStorageBrowse(s, callback, storageName, "")
	case "info":
		handleStorageInfo(s, callback, storageName)
	case service.CmdClose:
		deleteMsg := tgbotapi.NewDeleteMessage(callback.Message.Chat.ID, callback.Message.MessageID)
		_, _ = s.Bot.Request(deleteMsg)
		_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "Closed"))
	}
}

func handleStorageSelect(s *service.BotService, callback *tgbotapi.CallbackQuery, storageName string) {
	text := fmt.Sprintf("💾 *Storage: %s*\n\n━━━━━━━━━━━━━━━━━━━━━━━━\n\nPilih aksi:", utils.EscapeMarkdownV2(storageName))

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, text)
	editMsg.ParseMode = tgbotapi.ModeMarkdownV2
	_, _ = s.Bot.Send(editMsg)
}

func handleStorageBrowse(s *service.BotService, callback *tgbotapi.CallbackQuery, storageName, path string) {
	remotePath := storageName + ":"
	if path != "" {
		remotePath = storageName + ":" + path
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	configPath := s.TaskManager.ConfigDir + "/rclone.conf"

	cmd := execCommand(ctx, "rclone", "lsjson", remotePath, "--config", configPath)
	output, err := cmd.Output()

	if err != nil {
		_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, "❌ Failed to browse"))
		return
	}

	type DriveFile struct {
		Name  string `json:"Name"`
		IsDir bool   `json:"IsDir"`
	}

	var files []DriveFile
	_ = json.Unmarshal(output, &files)

	var text strings.Builder
	fmt.Fprintf(&text, "📂 *%s*\n", utils.EscapeMarkdownV2(remotePath))
	text.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	for i, f := range files {
		if i >= 15 {
			fmt.Fprintf(&text, "\n_\\.\\.\\. dan %d item lainnya_", len(files)-15)
			break
		}
		icon := "📄"
		if f.IsDir {
			icon = "📁"
		}
		fmt.Fprintf(&text, "%s `%s`\n", icon, utils.EscapeMarkdownV2Code(utils.TruncateString(f.Name, 40)))
	}

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, text.String())
	editMsg.ParseMode = tgbotapi.ModeMarkdownV2
	_, _ = s.Bot.Send(editMsg)
}

func handleStorageInfo(s *service.BotService, callback *tgbotapi.CallbackQuery, storageName string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	configPath := s.TaskManager.ConfigDir + "/rclone.conf"

	cmd := execCommand(ctx, "rclone", "about", storageName+":", "--config", configPath, "--json")
	output, err := cmd.Output()

	var text strings.Builder
	fmt.Fprintf(&text, "ℹ️ *Storage Info: %s*\n", utils.EscapeMarkdownV2(storageName))
	text.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

	if err != nil {
		text.WriteString("_Info tidak tersedia_\n")
	} else {
		var info map[string]interface{}
		if jsonErr := json.Unmarshal(output, &info); jsonErr == nil {
			if total, ok := info["total"].(float64); ok {
				fmt.Fprintf(&text, "📊 *Total:* `%s`\n", utils.EscapeMarkdownV2Code(utils.FormatBytes(int64(total))))
			}
			if used, ok := info["used"].(float64); ok {
				fmt.Fprintf(&text, "💾 *Used:* `%s`\n", utils.EscapeMarkdownV2Code(utils.FormatBytes(int64(used))))
			}
			if free, ok := info["free"].(float64); ok {
				fmt.Fprintf(&text, "✅ *Free:* `%s`\n", utils.EscapeMarkdownV2Code(utils.FormatBytes(int64(free))))
			}
		}
	}

	editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, text.String())
	editMsg.ParseMode = tgbotapi.ModeMarkdownV2
	_, _ = s.Bot.Send(editMsg)
}

func HandleSetStorage(s *service.BotService, message *tgbotapi.Message, args string) {
	if !s.IsAdmin(message.From.ID) {
		s.Reply(message, "❌ *Akses Ditolak*")
		return
	}

	if args == "" {
		s.Reply(message, "⚠️ *Format Salah*\n\nGunakan: `/setstorage remote:/path`\n\nContoh: `/setstorage gdrive:/MyMirror`")
		return
	}

	remoteName := strings.Split(args, ":")[0]
	providers, err := GetAvailableStorages(s)
	if err != nil {
		s.Reply(message, "❌ *Gagal validasi storage*")
		return
	}

	found := false
	for _, p := range providers {
		if p.Name == remoteName {
			found = true
			break
		}
	}

	if !found {
		s.Reply(message, fmt.Sprintf("❌ *Remote tidak ditemukan:* `%s`", utils.EscapeMarkdownV2(remoteName)))
		return
	}

	s.TaskManager.RcloneDest = args
	ctx := context.Background()
	_ = s.DB.Set(ctx, "rclone_dest", args)

	s.Reply(message, fmt.Sprintf("✅ *Storage Updated*\n\n📂 Destination: `%s`", utils.EscapeMarkdownV2Code(args)))
}

package file

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
	"zee-mirror/internal/service"
	"zee-mirror/pkg/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	IconFolder = "📂"
	IconFile   = "📄"
	CmdClose   = "close"
)

func HandleDriveList(s *service.BotService, message *tgbotapi.Message, args string, editMessageID int) {
	if !s.IsAuthorized(message.From.ID) {
		return
	}

	slog.Info("Drive list request", "userID", message.From.ID, "args", args, "editMsgID", editMessageID)

	fullPath, relPath := resolveDrivePath(s, args)
	slog.Debug("Listing drive path", "fullPath", fullPath)

	sent, err := sendLoadingStatus(s, message.Chat.ID, editMessageID)
	if err != nil {
		slog.Error("Error preparing status message", "error", err)
		return
	}

	files, err := listDriveFiles(s, fullPath)
	if err != nil {
		handleDriveError(s, message.Chat.ID, sent.MessageID, "Gagal memuat daftar file", err)
		return
	}

	slog.Debug("Drive list found items", "count", len(files))

	if tryJumpToFileInfo(s, message, sent.MessageID, relPath, files) {
		return
	}

	renderDriveList(s, message.Chat.ID, sent.MessageID, relPath, files)
}

func resolveDrivePath(s *service.BotService, args string) (string, string) {
	basePath := strings.TrimSuffix(s.TaskManager.RcloneDest, "/")
	fullPath := basePath

	if args != "" {
		if strings.HasPrefix(args, "/") {
			remoteName := strings.Split(s.TaskManager.RcloneDest, ":")[0]
			fullPath = remoteName + ":" + args
		} else {
			fullPath = basePath + "/" + strings.TrimPrefix(args, "/")
		}
	}
	fullPath = strings.TrimSuffix(fullPath, "/")

	baseRel := ""
	if strings.Contains(s.TaskManager.RcloneDest, ":") {
		parts := strings.SplitN(s.TaskManager.RcloneDest, ":", 2)
		if len(parts) > 1 {
			baseRel = strings.Trim(parts[1], "/")
		}
	}

	relPath := ""
	if strings.Contains(fullPath, ":") {
		parts := strings.SplitN(fullPath, ":", 2)
		if len(parts) > 1 {
			fullPathAfterRemote := strings.Trim(parts[1], "/")
			if baseRel != "" && (fullPathAfterRemote == baseRel || strings.HasPrefix(fullPathAfterRemote, baseRel+"/")) {
				relPath = strings.TrimPrefix(fullPathAfterRemote, baseRel)
				relPath = strings.TrimPrefix(relPath, "/")
			} else {
				relPath = "/" + fullPathAfterRemote
			}
		}
	}
	return fullPath, relPath
}

func sendLoadingStatus(s *service.BotService, chatID int64, editMessageID int) (*tgbotapi.Message, error) {
	loadingText := "🔍 *Memuat daftar file\\.\\.\\.*"
	if editMessageID != 0 {
		editMsg := tgbotapi.NewEditMessageText(chatID, editMessageID, loadingText)
		editMsg.ParseMode = tgbotapi.ModeMarkdownV2
		m, err := s.Bot.Send(editMsg)
		if err == nil {
			return &m, nil
		}
	}

	statusMsg := tgbotapi.NewMessage(chatID, loadingText)
	statusMsg.ParseMode = tgbotapi.ModeMarkdownV2
	m, err := s.Bot.Send(statusMsg)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func handleDriveError(s *service.BotService, chatID int64, messageID int, title string, err error) {
	slog.Error("Drive operation error", "title", title, "error", err)
	errorText := fmt.Sprintf("❌ *%s*\n\nError: %s", title, utils.EscapeMarkdownV2(err.Error()))
	editMsg := tgbotapi.NewEditMessageText(chatID, messageID, errorText)
	editMsg.ParseMode = tgbotapi.ModeMarkdownV2
	_, _ = s.Bot.Send(editMsg)
}

func tryJumpToFileInfo(s *service.BotService, message *tgbotapi.Message, messageID int, relPath string, files []service.DriveFile) bool {
	if len(files) == 1 && !files[0].IsDir {
		fileName := files[0].Name
		if relPath == fileName || strings.HasSuffix(relPath, "/"+fileName) {
			slog.Info("Target is a file, jumping to info view", "relPath", relPath, "messageID", messageID)
			handleDriveFileInfoDetailed(s, message.Chat.ID, messageID, relPath)
			return true
		}
	}
	return false
}

func renderDriveList(s *service.BotService, chatID int64, messageID int, relPath string, files []service.DriveFile) {
	currentPathForUI := relPath
	if !strings.HasPrefix(currentPathForUI, "/") {
		currentPathForUI = "/" + currentPathForUI
	}

	text := formatDriveFileList(currentPathForUI, files)
	keyboard := buildDriveNavigationKeyboard(s, files, relPath)

	editMsg := tgbotapi.NewEditMessageText(chatID, messageID, text)
	editMsg.ParseMode = tgbotapi.ModeMarkdownV2
	editMsg.ReplyMarkup = &keyboard

	if _, err := s.Bot.Send(editMsg); err != nil {
		slog.Error("Error sending final list message", "error", err)
		handleDriveError(s, chatID, messageID, "Gagal menampilkan daftar file", err)
	}
}

func listDriveFiles(s *service.BotService, path string) ([]service.DriveFile, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	configPath := s.TaskManager.ConfigDir + "/rclone.conf"
	args := []string{
		"lsjson",
		path,
		"--config", configPath,
		"--no-modtime",
	}

	cmd := exec.CommandContext(ctx, "rclone", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("rclone lsjson failed: %v", err)
	}

	var files []service.DriveFile
	if err := json.Unmarshal(output, &files); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	return files, nil
}

func formatDriveFileList(path string, files []service.DriveFile) string {
	var text strings.Builder

	text.WriteString("📂 *FILE MANAGER \\- GDRIVE*\n")
	text.WriteString("━━━━━━━━━━━━━━━━━━━━━━\n\n")
	fmt.Fprintf(&text, "📍 *PATH:* `%s`\n\n", utils.EscapeMarkdownV2Code(path))

	if len(files) == 0 {
		text.WriteString("📭 _Folder ini kosong_\n")
	} else {
		folderCount := 0
		fileCount := 0

		var folders []string
		for _, f := range files {
			if f.IsDir {
				folderCount++
				folders = append(folders, fmt.Sprintf("%s `%s`", IconFolder, utils.EscapeMarkdownV2Code(utils.TruncateString(f.Name, 40))))
			}
		}

		if len(folders) > 0 {
			text.WriteString("📁 *FOLDERS*\n")
			for _, f := range folders {
				text.WriteString(f + "\n")
			}
			text.WriteString("\n")
		}

		var fileList []string
		for _, f := range files {
			if !f.IsDir {
				fileCount++
				icon := getFileIcon(f.Name)
				size := utils.FormatBytes(f.Size)
				fileList = append(fileList, fmt.Sprintf("%s `%s` \\(%s\\)",
					icon,
					utils.EscapeMarkdownV2Code(utils.TruncateString(f.Name, 35)),
					utils.EscapeMarkdownV2(size)))
			}
		}

		if len(fileList) > 0 {
			text.WriteString("📄 *FILES*\n")
			for _, f := range fileList {
				text.WriteString(f + "\n")
			}
			text.WriteString("\n")
		}

		text.WriteString("📊 *SUMMARY*\n")
		fmt.Fprintf(&text, "📂 Folders: `%d`\n", folderCount)
		fmt.Fprintf(&text, "📄 Files: `%d`\n", fileCount)
	}

	text.WriteString("\n━━━━━━━━━━━━━━━━━━━━━━")
	return text.String()
}

func buildDriveNavigationKeyboard(_ *service.BotService, _ []service.DriveFile, _ string) tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.InlineKeyboardMarkup{}
}

func buildSearchNavigationKeyboard(_ *service.BotService, _ []service.DriveFile) tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.InlineKeyboardMarkup{}
}

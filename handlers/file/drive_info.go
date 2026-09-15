package file

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"zee-mirror/internal/service"
	"zee-mirror/pkg/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func handleDriveFileInfoDetailed(s *service.BotService, chatID int64, messageID int, relPath string) {
	targetPath, configPath := resolvePaths(s, relPath)

	slog.Debug("Detailed drive info request", "relPath", relPath, "targetPath", targetPath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	file := getDriveMetadata(ctx, targetPath, configPath)
	cloudLink := getCloudLink(ctx, s, targetPath, configPath)

	text := buildFileInfoMessage(relPath, file)
	keyboard := buildFileInfoKeyboard(s, relPath, cloudLink)

	s.SendOrEditMessage(chatID, messageID, text, keyboard)
}

func resolvePaths(s *service.BotService, relPath string) (string, string) {
	basePath := strings.TrimSuffix(s.TaskManager.RcloneDest, "/")
	cleanedRel := strings.Trim(relPath, "/")
	targetPath := basePath
	if cleanedRel != "" {
		targetPath = basePath + "/" + cleanedRel
	}
	configPath := s.TaskManager.ConfigDir + "/rclone.conf"
	return targetPath, configPath
}

func getDriveMetadata(ctx context.Context, targetPath, configPath string) service.DriveFile {
	cmd := exec.CommandContext(ctx, "rclone", "lsjson", targetPath, "--config", configPath)
	output, err := cmd.Output()
	if err != nil {
		slog.Error("rclone lsjson failed for info", "error", err, "path", targetPath)
		return service.DriveFile{}
	}

	var files []service.DriveFile
	if errJSON := json.Unmarshal(output, &files); errJSON == nil && len(files) > 0 {
		file := files[0]
		slog.Debug("Drive metadata found", "name", file.Name, "size", file.Size)
		return file
	}
	return service.DriveFile{}
}

func getCloudLink(ctx context.Context, s *service.BotService, targetPath, configPath string) string {
	if s.Config.IndexURL != "" {
		return generateIndexLink(s, targetPath)
	}

	linkCmd := exec.CommandContext(ctx, "rclone", "link", targetPath, "--config", configPath)
	linkOutput, _ := linkCmd.Output()
	return strings.TrimSpace(string(linkOutput))
}

func generateIndexLink(s *service.BotService, targetPath string) string {
	targetPathSlash := strings.ReplaceAll(targetPath, "\\", "/")
	rcloneDestSlash := strings.ReplaceAll(s.TaskManager.RcloneDest, "\\", "/")
	rcloneDestSlash = strings.TrimRight(rcloneDestSlash, "/")

	var relPath string
	if strings.HasPrefix(targetPathSlash, rcloneDestSlash) {
		relPath = strings.TrimPrefix(targetPathSlash, rcloneDestSlash)
	} else {
		parts := strings.SplitN(targetPathSlash, ":", 2)
		if len(parts) > 1 {
			relPath = parts[1]
		} else {
			relPath = targetPathSlash
		}
	}

	relPath = strings.TrimLeft(relPath, "/")
	pathParts := strings.Split(relPath, "/")
	for i, part := range pathParts {
		pathParts[i] = url.PathEscape(part)
	}
	encodedPath := strings.Join(pathParts, "/")
	baseURL := strings.TrimRight(s.Config.IndexURL, "/")
	return fmt.Sprintf("%s/%s", baseURL, encodedPath)
}

func buildFileInfoMessage(relPath string, file service.DriveFile) string {
	var text strings.Builder
	fmt.Fprintf(&text, "%s *FILE INFORMATION*\n", getFileIcon(relPath))
	text.WriteString("━━━━━━━━━━━━━━━━━━━━━━\n\n")
	fmt.Fprintf(&text, "📄 *Name:* `%s`\n", utils.EscapeMarkdownV2(filepath.Base(relPath)))

	if file.Name != "" {
		fmt.Fprintf(&text, "📦 *Size:* `%s`\n", utils.EscapeMarkdownV2(utils.FormatBytes(file.Size)))
		fmt.Fprintf(&text, "🕒 *Modified:* `%s`\n", utils.EscapeMarkdownV2(file.ModTime))
		fmt.Fprintf(&text, "🏷️ *MimeType:* `%s`\n", utils.EscapeMarkdownV2(file.MimeType))
	} else {
		text.WriteString("_Metadata limited atau rclone sedang sibuk_\n")
	}

	text.WriteString("\n━━━━━━━━━━━━━━━━━━━━━━")
	return text.String()
}

func buildFileInfoKeyboard(_ *service.BotService, _ string, _ string) tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.InlineKeyboardMarkup{}
}

func getFileIcon(filename string) string {
	lower := strings.ToLower(filename)

	switch {
	case strings.HasSuffix(lower, ".mp4"), strings.HasSuffix(lower, ".mkv"),
		strings.HasSuffix(lower, ".avi"), strings.HasSuffix(lower, ".mov"):
		return "🎬"
	case strings.HasSuffix(lower, ".mp3"), strings.HasSuffix(lower, ".flac"),
		strings.HasSuffix(lower, ".wav"), strings.HasSuffix(lower, ".aac"):
		return "🎵"
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"),
		strings.HasSuffix(lower, ".png"), strings.HasSuffix(lower, ".gif"):
		return "🖼️"
	case strings.HasSuffix(lower, ".pdf"):
		return "📕"
	case strings.HasSuffix(lower, ".doc"), strings.HasSuffix(lower, ".docx"):
		return IconFile
	case strings.HasSuffix(lower, ".xls"), strings.HasSuffix(lower, ".xlsx"):
		return "📊"
	case strings.HasSuffix(lower, ".zip"), strings.HasSuffix(lower, ".rar"),
		strings.HasSuffix(lower, ".7z"), strings.HasSuffix(lower, ".tar"):
		return "🗜️"
	case strings.HasSuffix(lower, ".exe"), strings.HasSuffix(lower, ".msi"):
		return "⚙️"
	case strings.HasSuffix(lower, ".iso"):
		return "💿"
	default:
		return IconFile
	}
}

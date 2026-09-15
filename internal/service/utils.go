package service

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"zee-mirror/internal/domain"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func GenerateThumbnail(ctx context.Context, videoPath, downloadDir string) (string, error) {
	videoPath = filepath.Clean(videoPath)
	if !filepath.IsAbs(videoPath) {
		return "", fmt.Errorf("%w: video path must be absolute", domain.ErrInvalidInput)
	}

	thumbnailPath := videoPath + ".jpg"

	allowedBaseDir := filepath.Clean(downloadDir)
	videoDir := filepath.Dir(videoPath)

	if !strings.HasPrefix(videoDir, allowedBaseDir) {
		return "", fmt.Errorf("%w: video path is not within allowed directory", domain.ErrUnauthorized)
	}

	cmd := execCommand(ctx, "ffmpeg", "-i", videoPath, "-ss", "00:00:05", "-vframes", "1", "-q:v", "2", thumbnailPath, "-y")
	if err := cmd.Run(); err != nil {
		cmd = execCommand(ctx, "ffmpeg", "-i", videoPath, "-ss", "00:00:00", "-vframes", "1", "-q:v", "2", thumbnailPath, "-y")
		if err := cmd.Run(); err != nil {
			return "", err
		}
	}
	return thumbnailPath, nil
}

func IsMediaMessage(msg *tgbotapi.Message) bool {
	if msg == nil {
		return false
	}
	return msg.Document != nil ||
		msg.Video != nil ||
		msg.Audio != nil ||
		msg.Voice != nil ||
		msg.VideoNote != nil ||
		msg.Animation != nil ||
		len(msg.Photo) > 0 ||
		msg.Sticker != nil
}

func (s *BotService) reply(message *tgbotapi.Message, text string) {
	msg := tgbotapi.NewMessage(message.Chat.ID, text)
	msg.ParseMode = MarkdownV2
	if _, err := s.Bot.Send(msg); err != nil {
		slog.Error("Failed to send message", "error", err, "userID", message.From.ID)
	}
}

func (s *BotService) EditMessage(chatID int64, msgID int, text string) {
	editMsg := tgbotapi.NewEditMessageText(chatID, msgID, text)
	editMsg.ParseMode = MarkdownV2
	_, err := s.Bot.Send(editMsg)
	if err != nil {
		slog.Warn("Failed to edit message", "error", err)
	}
}

func (s *BotService) EditMessageWithMarkup(chatID int64, msgID int, text string, _ *tgbotapi.InlineKeyboardMarkup) {
	editMsg := tgbotapi.NewEditMessageText(chatID, msgID, text)
	editMsg.ParseMode = MarkdownV2
	_, err := s.Bot.Send(editMsg)
	if err != nil {
		slog.Warn("Failed to edit message with markup", "error", err)
	}
}

func (s *BotService) GetFileFromMessage(message *tgbotapi.Message) (fileID, fileName string) {
	if message.ReplyToMessage == nil {
		return "", ""
	}
	msg := message.ReplyToMessage
	switch {
	case msg.Video != nil:
		return msg.Video.FileID, msg.Video.FileName
	case msg.Audio != nil:
		return msg.Audio.FileID, msg.Audio.FileName
	case msg.Document != nil:
		return msg.Document.FileID, msg.Document.FileName
	case msg.Animation != nil:
		return msg.Animation.FileID, msg.Animation.FileName
	case msg.Voice != nil:
		return msg.Voice.FileID, ""
	case msg.VideoNote != nil:
		return msg.VideoNote.FileID, ""
	}
	return "", ""
}

func (s *BotService) DownloadFile(url, destPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	cmd := execCommand(ctx, "curl", "-L", "-o", destPath, url)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: curl failed: %v\nOutput: %s", domain.ErrExternal, err, string(output))
	}
	return nil
}

func (s *BotService) GetFileLink(file tgbotapi.File, isOfficial bool) string {
	if s.Config.TelegramAPI != "" && !isOfficial {
		apiFormat := s.Config.TelegramAPI
		if strings.Contains(apiFormat, "/bot%s/%s") {
			apiFormat = strings.Replace(apiFormat, "/bot%s/%s", "/file/bot%s/%s", 1)
		} else {
			apiFormat = strings.TrimSuffix(apiFormat, "/") + "/file/bot%s/%s"
		}

		filePath := strings.TrimLeft(file.FilePath, "/")
		return fmt.Sprintf(apiFormat, s.Config.BotToken, filePath)
	}
	return file.Link(s.Config.BotToken)
}

func (s *BotService) ExtractFileFromReply(reply *tgbotapi.Message) (string, string, int64) {
	var fileID, fileName string
	var fileSize int64

	switch {
	case reply.Document != nil:
		fileID = reply.Document.FileID
		fileName = reply.Document.FileName
		fileSize = int64(reply.Document.FileSize)
	case reply.Video != nil:
		fileID = reply.Video.FileID
		fileName = reply.Video.FileName
		fileSize = int64(reply.Video.FileSize)
		if fileName == "" {
			fileName = fmt.Sprintf("video_%d.mp4", time.Now().Unix())
		}
	case reply.Audio != nil:
		fileID = reply.Audio.FileID
		fileName = reply.Audio.FileName
		fileSize = int64(reply.Audio.FileSize)
		if fileName == "" {
			fileName = fmt.Sprintf("audio_%d.mp3", time.Now().Unix())
		}
	case reply.Voice != nil:
		fileID = reply.Voice.FileID
		fileName = fmt.Sprintf("voice_%d.ogg", time.Now().Unix())
		fileSize = int64(reply.Voice.FileSize)
	case reply.VideoNote != nil:
		fileID = reply.VideoNote.FileID
		fileName = fmt.Sprintf("video_note_%d.mp4", time.Now().Unix())
		fileSize = int64(reply.VideoNote.FileSize)
	case reply.Animation != nil:
		fileID = reply.Animation.FileID
		fileName = reply.Animation.FileName
		fileSize = int64(reply.Animation.FileSize)
		if fileName == "" {
			fileName = fmt.Sprintf("animation_%d.mp4", time.Now().Unix())
		}
	case len(reply.Photo) > 0:
		photo := reply.Photo[len(reply.Photo)-1]
		fileID = photo.FileID
		fileName = fmt.Sprintf("photo_%d.jpg", time.Now().Unix())
		fileSize = int64(photo.FileSize)
	}

	return fileID, fileName, fileSize
}

func GetErrorMessage(title string, err any) string {
	var errMsg string
	switch e := err.(type) {
	case error:
		errMsg = e.Error()
	case string:
		errMsg = e
	default:
		errMsg = fmt.Sprintf("%v", e)
	}
	return fmt.Sprintf("❌ *%s*\n\n%s", title, errMsg)
}

func GetSuccessMessage(title, content string) string {
	return fmt.Sprintf("✅ *%s*\n\n%s", title, content)
}

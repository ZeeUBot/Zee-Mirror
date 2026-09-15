package media

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"zee-mirror/internal/service"
	"zee-mirror/pkg/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func GetErrorMessage(title, content string) string {
	return fmt.Sprintf("❌ *%s*\n\n%s", title, content)
}

func GetSuccessMessage(title, content string) string {
	return fmt.Sprintf("✅ *%s*\n\n%s", title, content)
}

func HandleExtractAudio(s *service.BotService, message *tgbotapi.Message, args string) {
	if !s.IsAuthorized(message.From.ID) {
		return
	}

	fileID, fileName := s.GetFileFromMessage(message)
	inputPath := args

	if fileID == "" && inputPath == "" {
		s.Reply(message, GetErrorMessage("INVALID FORMAT", "Reply ke file video dengan /extractaudio atau gunakan:\n`/extractaudio path`"))
		return
	}

	statusMsg := tgbotapi.NewMessage(message.Chat.ID, "🎵 *Extracting audio\\.\\.\\.*")
	statusMsg.ParseMode = tgbotapi.ModeMarkdownV2
	sent, err := s.Bot.Send(statusMsg)
	if err != nil {
		slog.Error("Failed to send status message", "error", err)
		s.Reply(message, "❌ *Gagal mengirim pesan status*")
		return
	}

	if fileID != "" {
		var downloadErr error
		inputPath, downloadErr = s.DownloadTelegramFile(fileID, fileName)
		if downloadErr != nil {
			slog.Error("Telegram download failed during audio extraction", "error", downloadErr)
			s.EditMessage(sent.Chat.ID, sent.MessageID, fmt.Sprintf("❌ *Gagal download file*\nError: %s", utils.EscapeMarkdownV2(downloadErr.Error())))
			return
		}
	}

	outputPath := strings.TrimSuffix(inputPath, filepath.Ext(inputPath)) + ".mp3"

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	if hasAudio, errAudio := s.HasAudioStream(inputPath); errAudio != nil || !hasAudio {
		s.EditMessage(sent.Chat.ID, sent.MessageID, GetErrorMessage("NO AUDIO", "Video ini tidak memiliki track audio untuk di\\-extract\\."))
		return
	}

	inputDir := filepath.Dir(inputPath)
	inputName := filepath.Base(inputPath)
	outputName := filepath.Base(outputPath)

	cmd := execCommand(ctx, "ffmpeg", "-i", inputName, "-vn", "-acodec", "libmp3lame", "-q:a", "2", outputName, "-y")
	cmd.Dir = inputDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		s.EditMessage(sent.Chat.ID, sent.MessageID, GetErrorMessage("FFMPEG ERROR", fmt.Sprintf("Gagal extract audio: %s\n\nOutput \\(tail\\):\n`%s`",
			utils.EscapeMarkdownV2(err.Error()),
			utils.EscapeMarkdownV2Code(utils.GetLastLines(string(output), 15)))))
		return
	}

	content := fmt.Sprintf("🎵 Output: `%s`\\!\n\nKlik tombol di bawah untuk upload ke Drive\\.", utils.EscapeMarkdownV2Code(filepath.Base(outputPath)))
	pathID := s.StorePath(outputPath)
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📤 Upload ke Drive", "media_m:"+pathID),
		),
	)
	s.EditMessageWithMarkup(sent.Chat.ID, sent.MessageID, GetSuccessMessage("AUDIO EXTRACTED", content), &keyboard)
}

func HandleCompressVideo(s *service.BotService, message *tgbotapi.Message, args string) {
	if !s.IsAuthorized(message.From.ID) {
		return
	}

	fileID, fileName := s.GetFileFromMessage(message)
	inputPath := args
	quality := "medium"

	if parts := strings.Fields(args); len(parts) > 0 {
		if fileID == "" {
			inputPath = parts[0]
			if len(parts) > 1 {
				quality = parts[1]
			}
		} else {
			quality = parts[0]
		}
	}

	if fileID == "" && inputPath == "" {
		s.Reply(message, "⚠️ *Format Salah*\n\nReply ke file video dengan /compress atau gunakan:\n`/compress path/to/video [quality]`\n\nQuality: low, medium, high \\(default: medium\\)")
		return
	}

	crf := "28"
	switch quality {
	case "low":
		crf = "32"
	case "high":
		crf = "23"
	}

	statusMsg := tgbotapi.NewMessage(message.Chat.ID, "🗜️ *Compressing video\\.\\.\\.*")
	statusMsg.ParseMode = tgbotapi.ModeMarkdownV2
	sent, err := s.Bot.Send(statusMsg)
	if err != nil {
		slog.Error("Failed to send status message", "error", err)
		s.Reply(message, "❌ *Gagal mengirim pesan status*")
		return
	}

	if fileID != "" {
		var downloadErr error
		inputPath, downloadErr = s.DownloadTelegramFile(fileID, fileName)
		if downloadErr != nil {
			s.EditMessage(sent.Chat.ID, sent.MessageID, "❌ *Gagal download file*")
			return
		}
	}

	ext := filepath.Ext(inputPath)
	outputPath := strings.TrimSuffix(inputPath, ext) + "_compressed" + ext

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	inputDir := filepath.Dir(inputPath)
	inputName := filepath.Base(inputPath)
	outputName := filepath.Base(outputPath)

	cmd := execCommand(ctx, "ffmpeg", "-i", inputName,
		"-c:v", "libx264", "-crf", crf, "-preset", "medium",
		"-c:a", "aac", "-b:a", "128k",
		outputName, "-y")
	cmd.Dir = inputDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		s.EditMessage(sent.Chat.ID, sent.MessageID, fmt.Sprintf("❌ *Gagal compress video*\n\nError: %s\n\nOutput \\(tail\\):\n`%s`",
			utils.EscapeMarkdownV2(err.Error()),
			utils.EscapeMarkdownV2Code(utils.GetLastLines(string(output), 15))))
		return
	}

	content := fmt.Sprintf("🎬 Output: `%s`\\!\n📊 Quality: %s\n\nKlik tombol di bawah untuk upload ke Drive\\.",
		utils.EscapeMarkdownV2Code(filepath.Base(outputPath)),
		utils.EscapeMarkdownV2(quality))
	pathID := s.StorePath(outputPath)
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📤 Upload ke Drive", "media_m:"+pathID),
		),
	)
	s.EditMessageWithMarkup(sent.Chat.ID, sent.MessageID, GetSuccessMessage("VIDEO COMPRESSED", content), &keyboard)
}

func HandleGenerateThumbnail(s *service.BotService, message *tgbotapi.Message, args string) {
	if !s.IsAuthorized(message.From.ID) {
		return
	}

	fileID, fileName := s.GetFileFromMessage(message)
	timestamp := "00:00:05"
	inputPath := ""

	if args != "" {
		parts := strings.Fields(args)
		if fileID != "" {
			timestamp = parts[0]
		} else {
			if len(parts) > 0 {
				inputPath = parts[0]
			}
			if len(parts) > 1 {
				timestamp = parts[1]
			}
		}
	}

	if fileID == "" && inputPath == "" {
		s.Reply(message, "⚠️ *Format Salah*\n\nReply ke file video dengan /thumbnail atau gunakan:\n`/thumbnail [timestamp]`\n\nContoh: `/thumbnail 00:01:30`")
		return
	}

	statusMsg := tgbotapi.NewMessage(message.Chat.ID, "🖼️ *Generating thumbnail\\.\\.\\.*")
	statusMsg.ParseMode = tgbotapi.ModeMarkdownV2
	sent, err := s.Bot.Send(statusMsg)
	if err != nil {
		slog.Error("Failed to send status message", "error", err)
		s.Reply(message, "❌ *Gagal mengirim pesan status*")
		return
	}

	if fileID != "" {
		var downloadErr error
		inputPath, downloadErr = s.DownloadTelegramFile(fileID, fileName)
		if downloadErr != nil {
			s.EditMessage(sent.Chat.ID, sent.MessageID, "❌ *Gagal download file*")
			return
		}
	}

	outputPath := strings.TrimSuffix(inputPath, filepath.Ext(inputPath)) + "_thumb.jpg"

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	inputDir := filepath.Dir(inputPath)
	inputName := filepath.Base(inputPath)
	outputName := filepath.Base(outputPath)

	cmd := execCommand(ctx, "ffmpeg", "-i", inputName,
		"-ss", timestamp, "-vframes", "1",
		"-q:v", "2", outputName, "-y")
	cmd.Dir = inputDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		s.EditMessage(sent.Chat.ID, sent.MessageID, fmt.Sprintf("❌ *Gagal generate thumbnail*\n\nError: %s\n\nOutput \\(tail\\):\n`%s`",
			utils.EscapeMarkdownV2(err.Error()),
			utils.EscapeMarkdownV2Code(utils.GetLastLines(string(output), 15))))
		return
	}

	photo := tgbotapi.NewPhoto(message.Chat.ID, tgbotapi.FilePath(outputPath))
	photo.Caption = fmt.Sprintf("🖼️ Thumbnail from `%s` at %s", utils.EscapeMarkdownV2Code(filepath.Base(inputPath)), utils.EscapeMarkdownV2(timestamp))
	photo.ParseMode = tgbotapi.ModeMarkdownV2
	_, _ = s.Bot.Send(photo)

	_, _ = s.Bot.Request(tgbotapi.NewDeleteMessage(sent.Chat.ID, sent.MessageID))
}

func HandleEmbedSubtitle(s *service.BotService, message *tgbotapi.Message, args string) {
	if !s.IsAuthorized(message.From.ID) {
		return
	}

	fileID, fileName := s.GetFileFromMessage(message)
	var videoPath, subPath string

	parts := strings.Fields(args)
	if fileID != "" {
		var err error
		videoPath, err = s.DownloadTelegramFile(fileID, fileName)
		if err != nil {
			s.Reply(message, "❌ *Gagal download file*")
			return
		}
		if len(parts) > 0 {
			subPath = parts[0]
		}
	} else {
		if len(parts) < 2 {
			s.Reply(message, "⚠️ *Format Salah*\n\nGunakan: `/subtitle video.mp4 subtitle.srt` atau reply ke video dengan `/subtitle subtitle.srt`")
			return
		}
		videoPath = parts[0]
		subPath = parts[1]
	}

	if videoPath == "" || subPath == "" {
		s.Reply(message, "⚠️ *Format Salah*\n\nGunakan: `/subtitle video.mp4 subtitle.srt` atau reply ke video dengan `/subtitle subtitle.srt`")
		return
	}

	statusMsg := tgbotapi.NewMessage(message.Chat.ID, "📝 *Embedding subtitle\\.\\.\\.*")
	statusMsg.ParseMode = tgbotapi.ModeMarkdownV2
	sent, err := s.Bot.Send(statusMsg)
	if err != nil {
		slog.Error("Failed to send status message", "error", err)
		s.Reply(message, "❌ *Gagal mengirim pesan status*")
		return
	}

	ext := filepath.Ext(videoPath)
	outputPath := strings.TrimSuffix(videoPath, ext) + "_subbed" + ext

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	inputDir := filepath.Dir(videoPath)

	cmd := execCommand(ctx, "ffmpeg", "-i", filepath.Base(videoPath),
		"-i", "file:"+subPath,
		"-c", "copy", "-c:s", "mov_text",
		filepath.Base(outputPath), "-y")
	cmd.Dir = inputDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		s.EditMessage(sent.Chat.ID, sent.MessageID, fmt.Sprintf("❌ *Gagal embed subtitle*\n\nError: %s\n\nOutput \\(tail\\):\n`%s`",
			utils.EscapeMarkdownV2(err.Error()),
			utils.EscapeMarkdownV2Code(utils.GetLastLines(string(output), 15))))
		return
	}

	content := fmt.Sprintf("🎬 Output: `%s`\\!\n\nKlik tombol di bawah untuk upload ke Drive\\.",
		utils.EscapeMarkdownV2Code(filepath.Base(outputPath)))
	pathID := s.StorePath(outputPath)
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📤 Upload ke Drive", "media_m:"+pathID),
		),
	)
	s.EditMessageWithMarkup(sent.Chat.ID, sent.MessageID, GetSuccessMessage("SUBTITLE EMBEDDED", content), &keyboard)
}

func HandleConvertFormat(s *service.BotService, message *tgbotapi.Message, args string) {
	if !s.IsAuthorized(message.From.ID) {
		return
	}

	fileID, fileName := s.GetFileFromMessage(message)
	var inputPath, targetFormat string

	parts := strings.Fields(args)
	if fileID != "" {
		var err error
		inputPath, err = s.DownloadTelegramFile(fileID, fileName)
		if err != nil {
			s.Reply(message, "❌ *Gagal download file*")
			return
		}
		if len(parts) > 0 {
			targetFormat = strings.ToLower(parts[0])
		}
	} else {
		if len(parts) < 2 {
			s.Reply(message, "⚠️ *Format Salah*\n\nGunakan: `/convert input.mp4 mkv` atau reply ke file with `/convert mkv`")
			return
		}
		inputPath = parts[0]
		targetFormat = strings.ToLower(parts[1])
	}

	validFormats := map[string]bool{
		"mp4": true, "mkv": true, "avi": true, "mov": true, "webm": true,
		"mp3": true, "aac": true, "flac": true, "wav": true,
	}

	if !validFormats[targetFormat] {
		s.Reply(message, "❌ *Format tidak didukung*\n\nSupported: mp4, mkv, avi, mov, webm, mp3, aac, flac, wav")
		return
	}

	statusMsg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("🔄 *Converting to %s\\.\\.\\.*", targetFormat))
	statusMsg.ParseMode = tgbotapi.ModeMarkdownV2
	sent, err := s.Bot.Send(statusMsg)
	if err != nil {
		slog.Error("Failed to send status message", "error", err)
		s.Reply(message, "❌ *Gagal mengirim pesan status*")
		return
	}

	outputPath := strings.TrimSuffix(inputPath, filepath.Ext(inputPath)) + "." + targetFormat

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	inputDir := filepath.Dir(inputPath)
	var cmd *exec.Cmd
	switch targetFormat {
	case "mp3", "aac", "flac", "wav":

		cmd = execCommand(ctx, "ffmpeg", "-i", filepath.Base(inputPath), "-vn", filepath.Base(outputPath), "-y")
	default:

		cmd = execCommand(ctx, "ffmpeg", "-i", filepath.Base(inputPath), "-c:v", "copy", "-c:a", "copy", filepath.Base(outputPath), "-y")
	}
	cmd.Dir = inputDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		s.EditMessage(sent.Chat.ID, sent.MessageID, fmt.Sprintf("❌ *Gagal convert format*\n\nError: %s\n\nOutput \\(tail\\):\n`%s`",
			utils.EscapeMarkdownV2(err.Error()),
			utils.EscapeMarkdownV2Code(utils.GetLastLines(string(output), 15))))
		return
	}

	content := fmt.Sprintf("📄 Output: `%s`\\!\n\nKlik tombol di bawah untuk upload ke Drive\\.",
		utils.EscapeMarkdownV2Code(filepath.Base(outputPath)))
	pathID := s.StorePath(outputPath)
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📤 Upload ke Drive", "media_m:"+pathID),
		),
	)
	s.EditMessageWithMarkup(sent.Chat.ID, sent.MessageID, GetSuccessMessage("MEDIA CONVERTED", content), &keyboard)
}

func HandleMediaInfo(s *service.BotService, message *tgbotapi.Message, args string) {
	if !s.IsAuthorized(message.From.ID) {
		return
	}

	fileID, fileName := s.GetFileFromMessage(message)
	inputPath := args

	if fileID != "" {
		var err error
		inputPath, err = s.DownloadTelegramFile(fileID, fileName)
		if err != nil {
			s.Reply(message, "❌ *Gagal download file*")
			return
		}
	}

	if inputPath == "" {
		s.Reply(message, "⚠️ *Format Salah*\n\nReply ke file media atau gunakan:\n`/mediainfo path/to/file`")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := execCommand(ctx, "ffprobe", "-v", "quiet", "-print_format", "json", "-show_format", "-show_streams", inputPath)
	output, err := cmd.Output()
	if err != nil {
		s.Reply(message, fmt.Sprintf("❌ *Gagal mendapatkan info*\n\nError: %s", utils.EscapeMarkdownV2(err.Error())))
		return
	}

	var probe service.FFProbeOutput
	if err := json.Unmarshal(output, &probe); err != nil {
		slog.Error("FFProbe JSON unmarshal failed", "error", err)
		s.Reply(message, "❌ *Gagal menguraikan metadata file*")
		return
	}

	text := s.FormatMediaInfo(filepath.Base(inputPath), probe)
	s.Reply(message, text)
}

func HandleMediaMirrorCallback(s *service.BotService, cb *tgbotapi.CallbackQuery, parts []string) {
	if len(parts) < 2 {
		return
	}

	pathID := parts[1]
	fullPath, ok := s.GetPath(pathID)
	if !ok {
		_, _ = s.Bot.Request(tgbotapi.NewCallback(cb.ID, "❌ Link kadaluarsa atau tidak valid!"))
		return
	}

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		_, _ = s.Bot.Request(tgbotapi.NewCallback(cb.ID, "❌ File sudah dihapus dari server!"))
		return
	}

	_, _ = s.Bot.Request(tgbotapi.NewCallback(cb.ID, "📤 Memulai mirror ke Drive..."))

	s.TaskManager.Mu.Lock()
	s.TaskManager.LastStatusMsg[cb.Message.Chat.ID] = cb.Message.MessageID
	s.TaskManager.Mu.Unlock()

	url := "file://" + fullPath
	fileName := filepath.Base(fullPath)

	replyID := 0
	if cb.Message.ReplyToMessage != nil {
		replyID = cb.Message.ReplyToMessage.MessageID
	}

	task, err := s.TaskManager.CreateTask(service.TypeMirror, url, fileName, cb.Message.Chat.ID, 0, replyID, cb.From.ID, false, false, "", "", 0, "", false)
	if err != nil {
		s.HandleCreateTaskError(cb.Message.Chat.ID, cb.Message.MessageID, err)
		return
	}
	s.HandleAutoDelete(task)
	s.UpdateSharedDashboard(cb.Message.Chat.ID, false)
	slog.Info("Local media mirror task created", "taskID", task.ID, "path", fullPath)
}

func HandleScreenshots(s *service.BotService, message *tgbotapi.Message, args string) {
	if !s.IsAuthorized(message.From.ID) {
		return
	}

	fileID, fileName := s.GetFileFromMessage(message)
	count := 4
	inputPath := args

	parts := strings.Fields(args)
	if fileID != "" {
		var err error
		inputPath, err = s.DownloadTelegramFile(fileID, fileName)
		if err != nil {
			s.Reply(message, "❌ *Gagal download file*")
			return
		}
		if len(parts) > 0 {
			_, _ = fmt.Sscanf(parts[0], "%d", &count)
		}
	} else if len(parts) > 0 {
		inputPath = parts[0]
		if len(parts) > 1 {
			_, _ = fmt.Sscanf(parts[1], "%d", &count)
		}
	}

	if count > 10 {
		count = 10
	} else if count < 1 {
		count = 1
	}

	if inputPath == "" {
		s.Reply(message, "⚠️ *Format Salah*\n\nGunakan: `/screenshots video.mp4 [count]` atau reply ke video dengan `/screenshots [count]`")
		return
	}

	statusMsg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("📸 *Generating %d screenshots\\.\\.\\.*", count))
	statusMsg.ParseMode = tgbotapi.ModeMarkdownV2
	sent, err := s.Bot.Send(statusMsg)
	if err != nil {
		slog.Error("Failed to send status message during screenshots", "error", err)
		s.Reply(message, "❌ *Gagal mengirim pesan status*")
		return
	}

	screenshots, err := s.GenerateScreenshotsList(inputPath, count)
	if err != nil {
		s.EditMessage(sent.Chat.ID, sent.MessageID, fmt.Sprintf("❌ *Gagal generate screenshots*\n\nError: %s", utils.EscapeMarkdownV2(err.Error())))
		return
	}

	var mediaGroup []interface{}
	for _, ss := range screenshots {
		photo := tgbotapi.NewInputMediaPhoto(tgbotapi.FilePath(ss))
		mediaGroup = append(mediaGroup, photo)
	}

	mediaMsg := tgbotapi.NewMediaGroup(message.Chat.ID, mediaGroup)
	_, _ = s.Bot.Send(mediaMsg)

	_, _ = s.Bot.Request(tgbotapi.NewDeleteMessage(sent.Chat.ID, sent.MessageID))
}

func HandleHardsub(s *service.BotService, message *tgbotapi.Message, args string) {
	if !s.IsAuthorized(message.From.ID) {
		return
	}

	fileID, fileName := s.GetFileFromMessage(message)
	var videoPath, subPath string

	parts := strings.Fields(args)
	if fileID != "" {
		var err error
		videoPath, err = s.DownloadTelegramFile(fileID, fileName)
		if err != nil {
			s.Reply(message, "❌ *Gagal download file*")
			return
		}
		if len(parts) > 0 {
			subPath = parts[0]
		}
	} else {
		if len(parts) < 2 {
			s.Reply(message, "⚠️ *Format Salah*\n\nGunakan: `/hardsub video.mp4 subtitle.srt` atau reply ke video dengan `/hardsub subtitle.srt`")
			return
		}
		videoPath = parts[0]
		subPath = parts[1]
	}

	if videoPath == "" || subPath == "" {
		s.Reply(message, "⚠️ *Format Salah*\n\nGunakan: `/hardsub video.mp4 subtitle.srt` atau reply ke video dengan `/hardsub subtitle.srt`")
		return
	}

	statusMsg := tgbotapi.NewMessage(message.Chat.ID, "🔥 *Burning subtitle (Hard-sub)...*\n_Proses ini memakan waktu lebih lama karena harus re-encoding._")
	statusMsg.ParseMode = tgbotapi.ModeMarkdownV2
	sent, err := s.Bot.Send(statusMsg)
	if err != nil {
		slog.Error("Failed to send status message", "error", err)
		return
	}

	ext := filepath.Ext(videoPath)
	outputPath := strings.TrimSuffix(videoPath, ext) + "_hardsub" + ext

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	absSubPath, _ := filepath.Abs(subPath)
	ffmpegSubPath := strings.ReplaceAll(absSubPath, "\\", "/")
	ffmpegSubPath = strings.ReplaceAll(ffmpegSubPath, ":", "\\:")

	cmd := execCommand(ctx, "ffmpeg", "-i", videoPath,
		"-vf", fmt.Sprintf("subtitles='%s'", ffmpegSubPath),
		"-c:v", "libx264", "-crf", "23", "-preset", "medium",
		"-c:a", "copy",
		outputPath, "-y")

	output, err := cmd.CombinedOutput()
	if err != nil {
		s.EditMessage(sent.Chat.ID, sent.MessageID, fmt.Sprintf("❌ *Gagal burn subtitle*\n\nError: %s\n\nOutput (tail):\n`%s`",
			utils.EscapeMarkdownV2(err.Error()),
			utils.EscapeMarkdownV2Code(utils.GetLastLines(string(output), 15))))
		return
	}

	content := fmt.Sprintf("🎬 Output: `%s`\\!\n\nKlik tombol di bawah untuk upload ke Drive\\.",
		utils.EscapeMarkdownV2Code(filepath.Base(outputPath)))
	pathID := s.StorePath(outputPath)
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📤 Upload ke Drive", "media_m:"+pathID),
		),
	)
	s.EditMessageWithMarkup(sent.Chat.ID, sent.MessageID, GetSuccessMessage("SUBTITLE BURNED", content), &keyboard)
}

func HandleRescale(s *service.BotService, message *tgbotapi.Message, args string) {
	if !s.IsAuthorized(message.From.ID) {
		return
	}

	fileID, fileName := s.GetFileFromMessage(message)
	var inputPath, resolution string

	parts := strings.Fields(args)
	if fileID != "" {
		if len(parts) == 0 {
			s.Reply(message, "⚠️ *Format Salah*\n\nReply ke video dengan `/rescale 1280x720` atau `/rescale 720p`")
			return
		}
		var err error
		inputPath, err = s.DownloadTelegramFile(fileID, fileName)
		if err != nil {
			s.Reply(message, "❌ *Gagal download file*")
			return
		}
		resolution = parts[0]
	} else {
		if len(parts) < 2 {
			s.Reply(message, "⚠️ *Format Salah*\n\nGunakan: `/rescale video.mp4 1280x720` atau `/rescale video.mp4 720p`")
			return
		}
		inputPath = parts[0]
		resolution = parts[1]
	}

	scale := ""
	switch strings.ToLower(resolution) {
	case "4k":
		scale = "3840:2160"
	case "2k":
		scale = "2560:1440"
	case "1080p":
		scale = "1920:1080"
	case "720p":
		scale = "1280:720"
	case "480p":
		scale = "854:480"
	case "360p":
		scale = "640:360"
	default:
		switch {
		case strings.Contains(resolution, "x"):
			scale = strings.Replace(resolution, "x", ":", 1)
		case strings.Contains(resolution, ":"):
			scale = resolution
		default:
			s.Reply(message, "❌ *Resolusi tidak valid*\n\nGunakan format: `1280x720` atau `720p`")
			return
		}
	}

	statusMsg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("📐 *Rescaling video to %s\\.\\.\\.*", resolution))
	statusMsg.ParseMode = tgbotapi.ModeMarkdownV2
	sent, err := s.Bot.Send(statusMsg)
	if err != nil {
		slog.Error("Failed to send status message", "error", err)
		return
	}

	ext := filepath.Ext(inputPath)
	outputPath := strings.TrimSuffix(inputPath, ext) + "_" + resolution + ext

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	cmd := execCommand(ctx, "ffmpeg", "-i", inputPath,
		"-vf", fmt.Sprintf("scale=%s:force_original_aspect_ratio=decrease,pad=%s:(ow-iw)/2:(oh-ih)/2", scale, scale),
		"-c:v", "libx264", "-crf", "23", "-preset", "medium",
		"-c:a", "copy",
		outputPath, "-y")

	output, err := cmd.CombinedOutput()
	if err != nil {
		s.EditMessage(sent.Chat.ID, sent.MessageID, fmt.Sprintf("❌ *Gagal rescale video*\n\nError: %s\n\nOutput (tail):\n`%s`",
			utils.EscapeMarkdownV2(err.Error()),
			utils.EscapeMarkdownV2Code(utils.GetLastLines(string(output), 15))))
		return
	}

	content := fmt.Sprintf("🎬 Output: `%s`\\!\n📐 Resolution: %s\n\nKlik tombol di bawah untuk upload ke Drive\\.",
		utils.EscapeMarkdownV2Code(filepath.Base(outputPath)),
		utils.EscapeMarkdownV2(resolution))
	pathID := s.StorePath(outputPath)
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📤 Upload ke Drive", "media_m:"+pathID),
		),
	)
	s.EditMessageWithMarkup(sent.Chat.ID, sent.MessageID, GetSuccessMessage("VIDEO RESCALED", content), &keyboard)
}

package ytdlp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
	"zee-mirror/internal/config"
	"zee-mirror/internal/domain"
	"zee-mirror/internal/downloader"
	"zee-mirror/internal/parser"
	"zee-mirror/pkg/utils"
	"zee-mirror/plugins/registry"
)

func init() {
	registry.RegisterMediaDownloader("ytdlp", func(cfg *config.Config) downloader.MediaDownloader {
		return NewEngine(cfg.ConfigDir)
	})
}

type Engine struct {
	ConfigDir string
}

type VideoFormat struct {
	Height int
	FPS    float64
}

func NewEngine(configDir string) *Engine {
	return &Engine{
		ConfigDir: configDir,
	}
}

func (e *Engine) GetFormats(ctx context.Context, url string) (map[int]downloader.FormatInfo, error) {
	args := []string{
		"-j",
		"--no-playlist",
		"--no-check-certificate",
		"--extractor-args", "youtube:player-client=web_creator,android_vr,ios,android",
		"--socket-timeout", "60",
		"--add-header", "Accept-Language: en-US,en;q=0.9",
		"--add-header", "Referer: https://www.youtube.com/",
		"--js-runtime", "node",
		"--remote-components", "ejs:github",
		"--playlist-items", "0",
	}

	cookiesPath := filepath.Join(e.ConfigDir, "cookies.txt")
	if _, err := os.Stat(cookiesPath); err == nil {
		args = append(args, "--cookies", cookiesPath)
		args = append(args, "--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36")
	}

	args = append(args, url)
	output, err := e.runYTDLP(ctx, args...)
	if err != nil {
		slog.Warn("YTDLP analysis first attempt failed, retrying with fallback clients...", "error", err)
		for i, arg := range args {
			if arg == "youtube:player-client=web_creator,android_vr,ios,android" {
				args[i] = "youtube:player-client=web_creator,android_vr"
				break
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
		output, err = e.runYTDLP(ctx, args...)
		if err != nil {
			return nil, err
		}
	}

	var data struct {
		Formats []struct {
			FormatID       string  `json:"format_id"`
			VCodec         string  `json:"vcodec"`
			ACodec         string  `json:"acodec"`
			FPS            float64 `json:"fps"`
			Height         int     `json:"height"`
			Filesize       int64   `json:"filesize"`
			FilesizeApprox int64   `json:"filesize_approx"`
		} `json:"formats"`
	}

	if err := json.Unmarshal(output, &data); err != nil {
		return nil, err
	}

	var bestAudioSize int64
	var bestVideoSize int64
	for _, f := range data.Formats {
		size := f.Filesize
		if size == 0 {
			size = f.FilesizeApprox
		}
		if f.VCodec == "none" && f.ACodec != "none" {
			if size > bestAudioSize {
				bestAudioSize = size
			}
		}
	}

	resMap := make(map[int]downloader.FormatInfo)
	for _, f := range data.Formats {
		if strings.HasPrefix(f.FormatID, "sb") {
			continue
		}

		size := f.Filesize
		if size == 0 {
			size = f.FilesizeApprox
		}

		if f.Height > 0 && f.VCodec != "" && f.VCodec != "none" {
			totalSize := size
			if f.ACodec == "none" {
				if size > 0 && bestAudioSize > 0 {
					totalSize = size + bestAudioSize
				} else {
					totalSize = 0
				}
			}
			if totalSize > bestVideoSize {
				bestVideoSize = totalSize
			}

			existing, ok := resMap[f.Height]
			if !ok || f.FPS > existing.FPS || (f.FPS == existing.FPS && totalSize > existing.Size) {
				resMap[f.Height] = downloader.FormatInfo{
					FPS:  f.FPS,
					Size: totalSize,
				}
			}
		}
	}

	if bestAudioSize > 0 {
		resMap[0] = downloader.FormatInfo{Size: bestAudioSize}
	}
	if bestVideoSize > 0 {
		resMap[-1] = downloader.FormatInfo{Size: bestVideoSize}
	}

	return resMap, nil
}

func (e *Engine) GetPlaylistMetadata(ctx context.Context, url string) (*downloader.PlaylistMetadata, error) {
	args := []string{
		"--flat-playlist",
		"-J",
		"--no-check-certificate",
		"--extractor-args", "youtube:player-client=web_creator,android_vr,ios,android",
		"--socket-timeout", "60",
		"--js-runtime", "node",
		"--remote-components", "ejs:github",
	}

	cookiesPath := filepath.Join(e.ConfigDir, "cookies.txt")
	if _, err := os.Stat(cookiesPath); err == nil {
		args = append(args, "--cookies", cookiesPath)
	}

	output, err := e.runYTDLP(ctx, args...)
	if err != nil {
		slog.Warn("YTDLP playlist first attempt failed, retrying...", "error", err, "url", url)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
		output, err = e.runYTDLP(ctx, args...)
		if err != nil {
			return nil, err
		}
	}

	var metadata downloader.PlaylistMetadata
	if err := json.Unmarshal(output, &metadata); err != nil {
		var singleEntry downloader.PlaylistEntry
		if errJSON := json.Unmarshal(output, &singleEntry); errJSON == nil && singleEntry.ID != "" {
			return &downloader.PlaylistMetadata{
				Title:   singleEntry.Title,
				Entries: []downloader.PlaylistEntry{singleEntry},
			}, nil
		}
		return nil, err
	}

	return &metadata, nil
}

func (e *Engine) IsPlaylist(url string) bool {
	lower := strings.ToLower(url)
	return strings.Contains(lower, "playlist") || strings.Contains(lower, "list=") || strings.Contains(lower, "/channel/") || strings.Contains(lower, "/user/") || strings.Contains(lower, "/c/") || strings.Contains(lower, "@")
}

func (e *Engine) Download(ctx context.Context, task *domain.Task, outputDir string, onProgress func(downloader.ProgressUpdate)) error {
	if errDir := os.MkdirAll(outputDir, 0750); errDir != nil {
		return &domain.StorageError{Path: outputDir, Err: errDir}
	}

	args := e.buildYTDLPArgs(task, outputDir)
	cmd := execCommand(ctx, "yt-dlp", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return &domain.NetworkError{URL: task.URL, Err: fmt.Errorf("failed to get stdout pipe: %v", err)}
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return &domain.NetworkError{URL: task.URL, Err: fmt.Errorf("failed to get stderr pipe: %v", err)}
	}

	if err := cmd.Start(); err != nil {
		return &domain.NetworkError{URL: task.URL, Err: fmt.Errorf("yt-dlp failed to start: %v", err)}
	}

	errorOutput := ""
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "ERROR:") || strings.Contains(line, "WARNING:") {
				errorOutput += line + "\n"
			}
			if strings.Contains(line, "ERROR:") {
				onProgress(downloader.ProgressUpdate{Error: line})
			}
		}
	}()

	go e.parseProgress(stdout, onProgress)

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		if errorOutput != "" {
			lines := strings.Split(errorOutput, "\n")
			var errors []string
			for _, l := range lines {
				if strings.Contains(l, "ERROR:") {
					errors = append(errors, l)
				}
			}
			if len(errors) > 0 {
				return &domain.ExternalError{Tool: "yt-dlp", Err: fmt.Errorf("%s", strings.Join(errors, "\n"))}
			}
			return &domain.ExternalError{Tool: "yt-dlp", Err: fmt.Errorf("%s", errorOutput)}
		}
		return &domain.ExternalError{Tool: "yt-dlp", Err: err}
	}

	if task.Hardsub && task.SubtitleLangs != "" {
		onProgress(downloader.ProgressUpdate{Message: "Burning subtitles..."})
		if err := e.burnSubtitles(ctx, outputDir); err != nil {
			slog.Error("Failed to burn subtitles", "error", err)

			onProgress(downloader.ProgressUpdate{Error: "Hardsub failed: " + err.Error()})
		} else {
			onProgress(downloader.ProgressUpdate{Message: "Hardsubbing completed!"})
		}
	}

	return nil
}

func (e *Engine) burnSubtitles(ctx context.Context, dir string) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var videoFile, subFile string
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		name := f.Name()
		ext := strings.ToLower(filepath.Ext(name))
		switch ext {
		case ".mp4", ".mkv", ".webm":
			videoFile = filepath.Join(dir, name)
		case ".srt", ".vtt", ".ass":
			subFile = filepath.Join(dir, name)
		}
	}

	if videoFile == "" || subFile == "" {
		return fmt.Errorf("video or subtitle file not found for burning")
	}

	outputFile := strings.TrimSuffix(videoFile, filepath.Ext(videoFile)) + "_hardsub.mp4"

	escapedSubFile := strings.ReplaceAll(subFile, "\\", "/")
	escapedSubFile = strings.ReplaceAll(escapedSubFile, ":", "\\:")

	args := []string{
		"-i", videoFile,
		"-vf", fmt.Sprintf("subtitles='%s'", escapedSubFile),
		"-c:a", "copy",
		"-y",
		outputFile,
	}

	cmd := execCommand(ctx, "ffmpeg", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg failed: %v, output: %s", err, string(out))
	}

	_ = os.Remove(videoFile)
	_ = os.Remove(subFile)
	return os.Rename(outputFile, videoFile)
}

func (e *Engine) buildYTDLPArgs(task *domain.Task, outputDir string) []string {
	outputTemplate := "%(title)s.%(ext)s"
	if task.FileName != "" && task.FileName != "video" && task.FileName != "unknown_file" && task.FileName != "watch" {
		ext := filepath.Ext(task.FileName)
		if ext != "" {
			lowerExt := strings.ToLower(ext)
			switch {
			case lowerExt == ".m3u8":
				outputTemplate = strings.TrimSuffix(task.FileName, ext) + ".mp4"
			case task.Quality == "audio" && (lowerExt == ".webm" || lowerExt == ".mp4" || lowerExt == ".mkv"):
				outputTemplate = strings.TrimSuffix(task.FileName, ext) + ".%(ext)s"
			default:
				outputTemplate = task.FileName
			}
		} else {
			outputTemplate = task.FileName + ".%(ext)s"
		}
	}

	args := []string{
		"-o", filepath.Join(outputDir, outputTemplate),
		"--newline", "--no-playlist",
		"--continue",
		"--no-check-certificate",
		"--ignore-errors",
		"--extractor-args", "youtube:player-client=web_creator,android_vr,ios,android",
		"--socket-timeout", "120",
		"--concurrent-fragments", "16",
		"--buffer-size", "1M",
		"--add-header", "Accept-Language: en-US,en;q=0.9",
		"--add-header", "Referer: https://www.youtube.com/",
		"--js-runtime", "node",
		"--remote-components", "ejs:github",
		"--no-cache-dir",
	}

	if task.Quality != "audio" {
		args = append(args, "--merge-output-format", "mp4")
		args = append(args, "--format-sort", "res,fps,codec:vp9,vcodec,br")
	}

	if task.SubtitleLangs != "" {
		args = append(args, "--write-subs", "--write-auto-subs")
		args = append(args, "--sub-langs", task.SubtitleLangs)
		if !task.Hardsub {
			args = append(args, "--embed-subs")
		}
	}

	cookiesPath := filepath.Join(e.ConfigDir, "cookies.txt")
	if _, err := os.Stat(cookiesPath); err == nil {
		args = append(args, "--cookies", cookiesPath)
		args = append(args, "--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36")
	}

	if task.Quality != "" {
		if task.Quality == "audio" {
			args = append(args, "-f", "bestaudio/best")
			args = append(args, "-x", "--audio-format", "mp3", "--audio-quality", "4")
		} else {
			format := fmt.Sprintf("bestvideo[height<=?%[1]s]+bestaudio/best[height<=?%[1]s]/best[height<=?%[1]s]", task.Quality)
			args = append(args, "-f", format)
		}
	}

	args = append(args, task.URL)
	return args
}

func (e *Engine) parseProgress(stdout interface{}, onProgress func(downloader.ProgressUpdate)) {
	reader, ok := stdout.(interface {
		Close() error
		Read(p []byte) (n int, err error)
	})
	if !ok {
		return
	}
	defer func() { _ = reader.Close() }()

	scanner := bufio.NewScanner(reader)
	scanner.Split(utils.ScanLinesWithCR)
	for scanner.Scan() {
		line := scanner.Text()
		update := downloader.ProgressUpdate{}

		if strings.Contains(line, "ERROR:") {
			update.Error = line
		}

		switch {
		case strings.HasPrefix(line, "[download] Destination:"):
			title := strings.TrimPrefix(line, "[download] Destination:")
			update.FileName = filepath.Base(strings.TrimSpace(title))
		case strings.HasPrefix(line, "[download]") && strings.Contains(line, "has already been downloaded"):
			title := strings.TrimPrefix(line, "[download]")
			title = strings.TrimSuffix(title, " has already been downloaded")
			update.FileName = filepath.Base(strings.TrimSpace(title))
		case strings.HasPrefix(line, "[ExtractAudio]"):
			update.Message = "Extracting/Converting Audio..."
			if strings.Contains(line, "Destination:") {
				parts := strings.SplitN(line, "Destination:", 2)
				update.FileName = filepath.Base(strings.TrimSpace(parts[1]))
			}
		case strings.HasPrefix(line, "[Merger]"):
			update.Message = "Merging Video/Audio..."
			if strings.Contains(line, "Merging formats into") {
				parts := strings.SplitN(line, "into", 2)
				update.FileName = filepath.Base(strings.TrimSpace(parts[1]))
				update.FileName = strings.Trim(update.FileName, "\"")
			}
		case strings.HasPrefix(line, "[VideoConvertor]"):
			update.Message = "Converting Video Format..."
			if strings.Contains(line, "Destination:") {
				parts := strings.SplitN(line, "Destination:", 2)
				update.FileName = filepath.Base(strings.TrimSpace(parts[1]))
			}
		case strings.HasPrefix(line, "[Metadata]"):
			update.Message = "Adding Metadata..."
		}

		p := parser.ParseYTDLPLine(line)
		if p.Found {
			update.Progress = p.Progress
			update.Total = p.Total
			update.Downloaded = p.Downloaded
			update.Speed = p.Speed
			update.ETA = p.ETA
			update.Connections = 16
		}

		if update.FileName != "" || update.Progress != 0 || update.Error != "" || update.Message != "" {
			slog.Debug("YTDLP parsed progress", "msg", update.Message, "file", update.FileName, "progress", update.Progress)
			onProgress(update)
		}
	}
}
func (e *Engine) runYTDLP(ctx context.Context, args ...string) ([]byte, error) {
	cmd := execCommand(ctx, "yt-dlp", args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		stderrStr := stderr.String()
		if stderrStr != "" {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderrStr))
		}
		return nil, err
	}
	return []byte(stdout.String()), nil
}

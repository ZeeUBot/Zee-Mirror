package drive

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"zee-mirror/internal/config"
	"zee-mirror/internal/domain"
	"zee-mirror/internal/downloader"
	"zee-mirror/pkg/utils"
	"zee-mirror/plugins/registry"
)

func init() {
	registry.RegisterDownloadEngine("drive", func(cfg *config.Config) downloader.DownloadEngine {
		return NewEngine(cfg)
	})
}

type Engine struct {
	Config *config.Config
}

func NewEngine(cfg *config.Config) *Engine {
	return &Engine{Config: cfg}
}

// CanHandle claims Google Drive URLs with an extractable file/folder ID.
// URLs without an ID fall through to the default engine (previous aria2 fallback).
func (e *Engine) CanHandle(url string) bool {
	if !strings.Contains(url, "drive.google.com") && !strings.Contains(url, "docs.google.com") && !strings.Contains(url, "drive.usercontent.google.com") {
		return false
	}
	id, _ := ExtractDriveID(url)
	return id != ""
}

func (e *Engine) Download(ctx context.Context, task *domain.Task, outputDir string, onProgress func(downloader.ProgressUpdate)) error {
	driveID, isFolderHint := ExtractDriveID(task.URL)
	if driveID == "" {
		return fmt.Errorf("no Google Drive ID in URL: %s", task.URL)
	}

	configPath := filepath.Join(e.Config.ConfigDir, "rclone.conf")
	remoteName := strings.Split(e.Config.RcloneDest, ":")[0]

	driveName, isDir, driveSize, err := GetDriveInfo(ctx, driveID, configPath, remoteName, isFolderHint, task.URL)
	if err != nil {
		slog.Warn("Failed to get Google Drive info", "error", err)
	}

	var name string
	task.Mu.Lock()
	if driveName != "" && (task.FileName == "" || task.FileName == "download" || utils.GetFileNameFromURL(task.URL) == task.FileName) {
		task.FileName = driveName
		if driveSize > 0 {
			task.TotalSize = driveSize
		}
	}
	name = task.FileName
	task.Mu.Unlock()

	if err := os.MkdirAll(outputDir, 0750); err != nil {
		return &domain.StorageError{Path: outputDir, Err: err}
	}

	args := buildRcloneDownloadArgs(e.Config, remoteName, driveID, outputDir, name, configPath, isDir)

	slog.Info("Starting Drive download via rclone", "taskID", task.ID, "args", strings.Join(args, " "))

	cmdCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := execCommand(cmdCtx, "rclone", args...)
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("rclone failed to start: %w", err)
	}

	scanner := bufio.NewScanner(stderr)
	scanner.Split(utils.ScanLinesWithCR)
	for scanner.Scan() {
		if p := ParseRcloneLine(scanner.Text()); p.HasSize || p.HasProgress || p.HasSpeed || p.HasETA {
			onProgress(downloader.ProgressUpdate{
				Downloaded: p.Downloaded,
				Total:      p.Total,
				Speed:      p.Speed,
				Progress:   p.Progress,
				ETA:        p.ETA,
			})
		}
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("rclone download failed: %w", err)
	}

	return nil
}

func buildRcloneDownloadArgs(cfg *config.Config, remoteName, driveID, outputDir, name, configPath string, isDir bool) []string {
	commonArgs := []string{
		"--config", configPath,
		"--progress",
		"--stats", "1s",
		"--stats-one-line",
		"--transfers", cfg.RcloneTransfers,
		"--checkers", cfg.RcloneCheckers,
		"--drive-chunk-size", cfg.RcloneDriveChunkSize,
		"--buffer-size", cfg.RcloneBufferSize,
		"--use-mmap",
		"--no-traverse",
		"--drive-pacer-min-sleep", cfg.RclonePacerMinSleep,
		"--drive-pacer-burst", cfg.RclonePacerBurst,
		"--drive-acknowledge-abuse",
		"--log-level", cfg.RcloneLogLevel,
	}

	var args []string
	if isDir {
		src := fmt.Sprintf("%s,root_folder_id=%s:", remoteName, driveID)
		dest := filepath.Join(outputDir, name)
		args = []string{
			"copy",
			src,
			dest,
		}
		args = append(args, commonArgs...)
	} else {
		src := fmt.Sprintf("%s,root_folder_id=%s:%s", remoteName, driveID, name)
		dest := filepath.Join(outputDir, name)
		args = []string{
			"copyto",
			src,
			dest,
		}
		args = append(args, commonArgs...)
	}

	return args
}

// LineProgress is the parsed content of one rclone stats line. Flags tell
// which fields the line actually carried, so callers never zero out state
// on non-matching lines.
type LineProgress struct {
	Downloaded, Total, Speed int64
	Progress                 float64
	ETA                      time.Duration
	HasSize, HasProgress     bool
	HasSpeed, HasETA         bool
}

var (
	rcloneSizeRegex     = regexp.MustCompile(`(\d+(?:\.\d+)?\s*[a-zA-Z]+i?B)\s*/\s*(\d+(?:\.\d+)?\s*[a-zA-Z]+i?B)`)
	rcloneProgressRegex = regexp.MustCompile(`(\d+(?:\.\d+)?)%`)
	rcloneSpeedRegex    = regexp.MustCompile(`(?i)(?:,|\s)(\d+(?:\.\d+)?\s*[a-zA-Z]+i?B/s)`)
	rcloneETARegex      = regexp.MustCompile(`(?i)(?:ETA\s+|,\s+)(\d+[smhd])`)
)

// ParseRcloneLine parses one rclone --stats-one-line stderr line.
func ParseRcloneLine(line string) LineProgress {
	var p LineProgress

	if matches := rcloneSizeRegex.FindStringSubmatch(line); len(matches) >= 3 {
		p.Downloaded = utils.ParseBytesString(matches[1])
		p.Total = utils.ParseBytesString(matches[2])
		p.HasSize = true
	}

	if matches := rcloneProgressRegex.FindStringSubmatch(line); len(matches) >= 2 {
		if pct, err := strconv.ParseFloat(matches[1], 64); err == nil {
			p.Progress = pct
			p.HasProgress = true
		}
	}

	if matches := rcloneSpeedRegex.FindStringSubmatch(line); len(matches) >= 2 {
		p.Speed = utils.ParseBytesString(matches[1])
		p.HasSpeed = true
	}

	if matches := rcloneETARegex.FindStringSubmatch(line); len(matches) >= 2 {
		if d, err := time.ParseDuration(matches[1]); err == nil {
			p.ETA = d
			p.HasETA = true
		}
	}

	return p
}

func ExtractDriveID(urlStr string) (string, bool) {
	folderRegex := regexp.MustCompile(`folders/([a-zA-Z0-9_-]+)`)
	if matches := folderRegex.FindStringSubmatch(urlStr); len(matches) >= 2 {
		return matches[1], true
	}

	fileRegex := regexp.MustCompile(`(?:d/|id=)([a-zA-Z0-9_-]+)`)
	if matches := fileRegex.FindStringSubmatch(urlStr); len(matches) >= 2 {
		return matches[1], false
	}

	return "", false
}

func GetDriveInfo(ctx context.Context, id, configPath, remoteName string, isFolder bool, urlStr string) (string, bool, int64, error) {
	idArgs := []string{
		"lsjson",
		fmt.Sprintf("%s,root_folder_id=%s:", remoteName, id),
		"--max-depth", "0",
		"--stat",
		"--config", configPath,
		"--no-mimetype",
		"--no-modtime",
	}
	idCmd := execCommand(ctx, "rclone", idArgs...)
	if out, err := idCmd.Output(); err == nil {
		var info map[string]interface{}
		if json.Unmarshal(out, &info) == nil {
			fetchedID, _ := info["ID"].(string)
			if fetchedID == "" {
				fetchedID, _ = info["Id"].(string)
			}
			if fetchedID == id {
				fileName, _ := info["Name"].(string)
				isDirVal, _ := info["IsDir"].(bool)
				sizeVal := int64(0)
				if s, ok := info["Size"].(float64); ok {
					sizeVal = int64(s)
				}
				if fileName != "" {
					slog.Debug("Resolved GDrive info from lsjson --stat", "id", id, "name", fileName, "isDir", isDirVal)
					return fileName, isDirVal, sizeVal, nil
				}
			} else {
				slog.Warn("Fetched ID (lsjson) does not match requested ID", "expected", id, "got", fetchedID)
			}
		} else {
			var infos []map[string]interface{}
			if json.Unmarshal(out, &infos) == nil && len(infos) > 0 {
				info := infos[0]
				fetchedID, _ := info["ID"].(string)
				if fetchedID == "" {
					fetchedID, _ = info["Id"].(string)
				}
				if fetchedID == id {
					fileName, _ := info["Name"].(string)
					isDirVal, _ := info["IsDir"].(bool)
					sizeVal := int64(0)
					if s, ok := info["Size"].(float64); ok {
						sizeVal = int64(s)
					}
					if fileName != "" {
						slog.Debug("Resolved GDrive info from lsjson list", "id", id, "name", fileName, "isDir", isDirVal)
						return fileName, isDirVal, sizeVal, nil
					}
				}
			}
		}
	}

	args := []string{
		"backend", "info",
		fmt.Sprintf("%s:", remoteName),
		"-o", fmt.Sprintf("root_folder_id=%s", id),
		"--config", configPath,
		"-o", "drive-acknowledge-abuse=true",
		"--json",
	}

	cmd := execCommand(ctx, "rclone", args...)
	output, err := cmd.Output()
	if err == nil {
		var info map[string]interface{}
		if unmarshalErr := json.Unmarshal(output, &info); unmarshalErr == nil {
			fetchedID, _ := info["id"].(string)
			if fetchedID == id {
				fileName, _ := info["name"].(string)
				mimeType, _ := info["mimeType"].(string)
				sizeVal := int64(0)
				if s, ok := info["size"].(float64); ok {
					sizeVal = int64(s)
				} else if sStr, ok := info["size"].(string); ok {
					sizeVal, _ = strconv.ParseInt(sStr, 10, 64)
				}

				isDir := strings.Contains(mimeType, "folder")

				if fileName != "" {
					slog.Debug("Resolved GDrive info from backend info", "id", id, "name", fileName, "isDir", isDir, "size", sizeVal)
					return fileName, isDir, sizeVal, nil
				}
			} else {
				slog.Warn("Fetched ID (backend info) does not match requested ID", "expected", id, "got", fetchedID)
			}
		}
	}

	scrapeURL := ConstructScrapeURL(id, isFolder, urlStr)
	name := getDriveNameFromURL(scrapeURL)
	if name != "" {
		slog.Debug("Resolved GDrive name from title", "id", id, "name", name)
		return name, isFolder, 0, nil
	}

	if isFolder {
		return "Folder_" + id, true, 0, nil
	}

	return "File_" + id, false, 0, nil
}

func getDriveNameFromURL(urlStr string) string {
	if strings.Contains(urlStr, "drive.usercontent.google.com") {
		return ""
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := client.Get(urlStr)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*100))
	if err != nil {
		return ""
	}

	re := regexp.MustCompile(`(?i)<title>(.*?)</title>`)
	matches := re.FindStringSubmatch(string(body))
	if len(matches) < 2 {
		return ""
	}

	title := matches[1]
	if idx := strings.Index(title, " - Google "); idx != -1 {
		title = title[:idx]
	}
	if strings.Contains(title, "Sign-in") || strings.Contains(title, "Masuk") || strings.Contains(title, "Virus scan") {
		return ""
	}

	return strings.TrimSpace(title)
}

func ConstructScrapeURL(id string, isFolder bool, originalURL string) string {
	if id == "" {
		return originalURL
	}

	if isFolder {
		return fmt.Sprintf("https://drive.google.com/drive/folders/%s", id)
	}
	return fmt.Sprintf("https://drive.google.com/file/d/%s/view", id)
}

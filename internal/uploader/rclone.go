package uploader

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"zee-mirror/internal/config"
	"zee-mirror/internal/domain"
	"zee-mirror/internal/metrics"
	"zee-mirror/internal/organizer"
	"zee-mirror/pkg/utils"
)

type ProgressUpdate struct {
	Error        string
	UploadedSize int64
	TotalSize    int64
	Progress     float64
	Speed        int64
	ETA          time.Duration
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

type FileUploader interface {
	Upload(ctx context.Context, task *domain.Task, onProgress func(ProgressUpdate)) error
}

type RcloneUploader struct {
	cfg *config.Config
}

func NewRcloneUploader(cfg *config.Config) *RcloneUploader {
	return &RcloneUploader{
		cfg: cfg,
	}
}

func (r *RcloneUploader) Upload(ctx context.Context, task *domain.Task, onProgress func(ProgressUpdate)) error {
	startTime := time.Now()

	uploadPath := task.LocalPath
	if uploadPath == "" {
		return &domain.ExternalError{Tool: "rclone", Err: fmt.Errorf("no file to upload")}
	}

	totalSize := task.TotalSize
	if info, err := os.Stat(uploadPath); err == nil {
		if info.IsDir() {
			dirSize, err := utils.CalculateDirSize(uploadPath)
			if err != nil {
				slog.Warn("Could not calculate directory size", "error", err, "path", uploadPath)
				totalSize = info.Size()
			} else {
				totalSize = dirSize
			}
		} else {
			totalSize = info.Size()
		}
	}

	remoteDest := r.cfg.RcloneDest
	if r.cfg.SmartAutoOrganization {
		if subFolder := organizer.GetTargetFolder(task.FileName); subFolder != "" {
			remoteDest = filepath.Join(remoteDest, subFolder)
			slog.Info("Smart Auto Organization: moving to subfolder", "taskID", task.ID, "subFolder", subFolder)
		}
	}

	remotePath := filepath.Join(remoteDest, task.FileName)
	task.RemotePath = remotePath

	rcloneDest := remoteDest
	if info, err := os.Stat(uploadPath); err == nil && info.IsDir() {
		rcloneDest = remotePath
	}

	configPath := filepath.Join(r.cfg.ConfigDir, "rclone.conf")
	args := []string{
		"copy",
		uploadPath,
		rcloneDest,
		"--config", configPath,
		// no --progress: its \r-overwrite output starves our line parser in Docker (watchdog killed healthy uploads)
		"--stats", "1s",
		"--stats-one-line",
		"--transfers", r.cfg.RcloneTransfers,
		"--checkers", r.cfg.RcloneCheckers,
		"--drive-chunk-size", r.cfg.RcloneDriveChunkSize,
		"--drive-upload-cutoff", r.cfg.RcloneDriveChunkSize,
		"--buffer-size", r.cfg.RcloneBufferSize,
		"--low-level-retries", "10",
		"--use-mmap",
		"--size-only",
		"--no-traverse",
		"--drive-pacer-min-sleep", r.cfg.RclonePacerMinSleep,
		"--drive-pacer-burst", r.cfg.RclonePacerBurst,
		"--log-level", r.cfg.RcloneLogLevel,
	}

	cmdCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "rclone", args...)

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return &domain.ExternalError{Tool: "rclone", Err: fmt.Errorf("failed to create stderr pipe: %v", err)}
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return &domain.ExternalError{Tool: "rclone", Err: fmt.Errorf("failed to create stdout pipe: %v", err)}
	}

	slog.Info("Starting rclone upload", "taskID", task.ID, "args", strings.Join(args, " "))

	err = cmd.Start()
	if err != nil {
		return &domain.ExternalError{Tool: "rclone", Err: fmt.Errorf("failed to start rclone: %v", err)}
	}

	done := make(chan struct{})
	progressDone := make(chan struct{})
	lastActivity := time.Now()
	var activityMu sync.Mutex
	var estimateWg sync.WaitGroup

	estimateWg.Add(1)
	go func() {
		defer estimateWg.Done()
		r.estimateUploadProgress(totalSize, onProgress, progressDone)
	}()

	wrappedOnProgress := func(up ProgressUpdate) {
		activityMu.Lock()
		lastActivity = time.Now()
		activityMu.Unlock()
		onProgress(up)
	}

	onActivity := func() {
		activityMu.Lock()
		lastActivity = time.Now()
		activityMu.Unlock()
	}

	go r.parseRcloneProgress(task.ID, stderrPipe, wrappedOnProgress, onActivity)
	go r.parseRcloneProgress(task.ID, stdoutPipe, wrappedOnProgress, onActivity)

	progressTimeout := 3 * time.Minute

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				activityMu.Lock()
				stuckFor := time.Since(lastActivity)
				activityMu.Unlock()
				if stuckFor > progressTimeout {
					slog.Error("Rclone upload stuck - no progress for too long, cancelling", "taskID", task.ID, "stuckFor", stuckFor)
					cancel()
					return
				}
			}
		}
	}()

	err = cmd.Wait()
	close(done)
	close(progressDone)
	estimateWg.Wait()

	if err != nil {
		metrics.UploadDuration.WithLabelValues("rclone", "failed").Observe(time.Since(startTime).Seconds())
		if task.Status == domain.StatusCancelled {
			return &domain.ExternalError{Tool: "rclone", Err: fmt.Errorf("task cancelled")}
		}
		return &domain.ExternalError{Tool: "rclone", Err: fmt.Errorf("rclone failed: %v", err)}
	}

	if task.Status == domain.StatusCancelled {
		metrics.UploadDuration.WithLabelValues("rclone", "failed").Observe(time.Since(startTime).Seconds())
		return &domain.ExternalError{Tool: "rclone", Err: fmt.Errorf("task cancelled")}
	}

	isDir := false
	if info, err := os.Stat(uploadPath); err == nil {
		isDir = info.IsDir()
	}
	r.GenerateRcloneLink(cmdCtx, task, configPath, isDir)

	onProgress(ProgressUpdate{Progress: 100, UploadedSize: totalSize, TotalSize: totalSize})
	metrics.UploadDuration.WithLabelValues("rclone", "success").Observe(time.Since(startTime).Seconds())

	if task.Dest2 != "" {
		if info, err := os.Stat(uploadPath); err == nil && !info.IsDir() {
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				f, err := os.Open(uploadPath)
				if err != nil {
					slog.Error("Failed to open file for Dest2 upload", "taskID", task.ID, "error", err)
					return
				}
				defer f.Close()
				rcloneDest2 := filepath.Join(task.Dest2, task.FileName)
				if err := r.UploadToCustomDest(ctx, f, task.FileName, rcloneDest2); err != nil {
					slog.Error("Failed to upload to Dest2", "taskID", task.ID, "dest2", task.Dest2, "error", err)
				}
			}()
			wg.Wait()
		}
	}

	return nil
}

func (r *RcloneUploader) UploadToCustomDest(ctx context.Context, content io.Reader, fileName, dest string) error {
	tmpFile, err := os.CreateTemp("", "rclone-upload-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, copyErr := io.Copy(tmpFile, content); copyErr != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write temp file: %v", copyErr)
	}
	if closeErr := tmpFile.Close(); closeErr != nil {
		return fmt.Errorf("failed to close temp file: %v", closeErr)
	}

	configPath := filepath.Join(r.cfg.ConfigDir, "rclone.conf")
	args := []string{
		"copy",
		tmpPath,
		dest,
		"--config", configPath,
		"--progress",
		"--stats", "1s",
		"--stats-one-line",
		"--transfers", r.cfg.RcloneTransfers,
		"--checkers", r.cfg.RcloneCheckers,
		"--drive-chunk-size", r.cfg.RcloneDriveChunkSize,
		"--drive-upload-cutoff", r.cfg.RcloneDriveChunkSize,
		"--buffer-size", r.cfg.RcloneBufferSize,
		"--low-level-retries", "10",
		"--use-mmap",
		"--size-only",
		"--no-traverse",
		"--drive-pacer-min-sleep", r.cfg.RclonePacerMinSleep,
		"--drive-pacer-burst", r.cfg.RclonePacerBurst,
		"--log-level", r.cfg.RcloneLogLevel,
	}

	cmd := exec.CommandContext(ctx, "rclone", args...)
	slog.Info("Starting rclone upload to custom dest", "dest", dest, "fileName", fileName, "args", strings.Join(args, " "))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return &domain.ExternalError{Tool: "rclone", Err: fmt.Errorf("rclone to %s failed: %v", dest, err), Output: string(output)}
	}
	slog.Info("Upload to custom dest completed", "dest", dest, "fileName", fileName)
	return nil
}

func (r *RcloneUploader) estimateUploadProgress(totalSize int64, onProgress func(ProgressUpdate), done chan struct{}) {
	const estimatedSpeedBytesPerSec = 3.5 * 1024 * 1024

	if totalSize <= 0 {
		return
	}

	estimatedTotalSeconds := float64(totalSize) / estimatedSpeedBytesPerSec
	if estimatedTotalSeconds < 3 {
		return
	}

	startTime := time.Now()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			elapsed := time.Since(startTime).Seconds()

			estimatedProgress := (elapsed / estimatedTotalSeconds) * 100
			if estimatedProgress > 95 {
				estimatedProgress = 95
			}

			estimatedUploaded := int64(elapsed * estimatedSpeedBytesPerSec)
			if estimatedUploaded > totalSize {
				estimatedUploaded = totalSize
			}

			remainingSeconds := estimatedTotalSeconds - elapsed
			if remainingSeconds < 0 {
				remainingSeconds = 0
			}

			onProgress(ProgressUpdate{
				Progress:     estimatedProgress,
				UploadedSize: estimatedUploaded,
				TotalSize:    totalSize,
				Speed:        int64(estimatedSpeedBytesPerSec),
				ETA:          time.Duration(remainingSeconds) * time.Second,
			})
		}
	}
}

func (r *RcloneUploader) GenerateRcloneLink(ctx context.Context, task *domain.Task, configPath string, isDirUpload bool) {
	currentRemotePath := task.RemotePath
	if currentRemotePath == "" {
		currentRemotePath = filepath.Join(r.cfg.RcloneDest, task.FileName)
	}

	currentRemotePath = strings.ReplaceAll(currentRemotePath, "\\", "/")

	if r.cfg.IndexURL != "" {
		if r.generateIndexURL(ctx, task, configPath, currentRemotePath) {
			return
		}
	}

	r.generateDirectLink(ctx, task, configPath, currentRemotePath, isDirUpload)
}

func (r *RcloneUploader) generateIndexURL(ctx context.Context, task *domain.Task, configPath, currentRemotePath string) bool {
	if r.generateIDBasedIndexURL(ctx, task, configPath, currentRemotePath) {
		return true
	}
	return r.generatePathBasedIndexURL(task, currentRemotePath)
}

func (r *RcloneUploader) generateIDBasedIndexURL(ctx context.Context, task *domain.Task, configPath, currentRemotePath string) bool {
	var fileID, parentID string

	lsArgs := []string{
		"lsjson",
		"--stat",
		currentRemotePath,
		"--config", configPath,
		"--no-modtime",
		"--no-mimetype",
	}

	var lsOutput []byte
	var err error

	for i := 0; i < 2; i++ {
		lsCmd := exec.CommandContext(ctx, "rclone", lsArgs...)
		lsOutput, err = lsCmd.Output()
		if err == nil {
			break
		}
		if !sleepCtx(ctx, time.Second) {
			break
		}
	}

	if err == nil {
		var entry map[string]interface{}
		if json.Unmarshal(lsOutput, &entry) == nil {
			if id, ok := entry["ID"].(string); ok {
				fileID = id
			} else if id, ok := entry["Id"].(string); ok {
				fileID = id
			}
		} else {
			var files []map[string]interface{}
			if json.Unmarshal(lsOutput, &files) == nil && len(files) > 0 {
				if id, ok := files[0]["ID"].(string); ok {
					fileID = id
				} else if id, ok := files[0]["Id"].(string); ok {
					fileID = id
				}
			}
		}
	} else {
		slog.Warn("Failed to get File ID directly, trying fallback via parent listing", "path", currentRemotePath, "error", err)
	}

	parentPath := path.Dir(currentRemotePath)

	if fileID == "" {
		slog.Info("Attempting fallback to find file ID in parent directory", "parentPath", parentPath)
		params := []string{
			"lsjson",
			parentPath,
			"--config", configPath,
			"--no-modtime",
			"--no-mimetype",
			"--depth", "1",
		}

		for i := 0; i < 3; i++ {
			cmd := exec.CommandContext(ctx, "rclone", params...)
			out, errFallback := cmd.Output()
			if errFallback == nil {
				var files []map[string]interface{}
				if json.Unmarshal(out, &files) == nil {
					targetName := task.FileName
					var foundNames []string

					for _, f := range files {
						name, nameOk := f["Name"].(string)
						pathVal, pathOk := f["Path"].(string)
						if nameOk {
							if i == 0 || i > 10 {
								foundNames = append(foundNames, name)
							}
						} else if pathOk {
							if i == 0 || i > 10 {
								foundNames = append(foundNames, path.Base(pathVal))
							}
						}

						candidate := name
						if candidate == "" && pathOk {
							candidate = path.Base(pathVal)
						}

						if candidate == targetName {
							if id, ok := f["ID"].(string); ok {
								fileID = id
							} else if id, ok := f["Id"].(string); ok {
								fileID = id
							}
							break
						}
					}

					if len(foundNames) > 0 {
						slog.Info("Files found in parent during fallback", "iteration", i, "count", len(files), "sample", strings.Join(foundNames[:min(len(foundNames), 5)], ", "))
					}
				}
				if fileID != "" {
					slog.Info("Found File ID via fallback parent listing", "fileID", fileID)
					break
				}
			} else {
				slog.Debug("Fallback lsjson returned error", "error", errFallback)
			}
			if !sleepCtx(ctx, time.Second) {
				break
			}
		}
	}
	if strings.HasSuffix(parentPath, ":") || parentPath == "." {
		if strings.HasSuffix(parentPath, ":") {
			rootLsArgs := []string{
				"lsjson",
				parentPath,
				"--config", configPath,
				"--no-modtime",
				"--no-mimetype",
				"--depth", "0",
			}
			rootLsCmd := exec.CommandContext(ctx, "rclone", rootLsArgs...)
			if rootLsOutput, err := rootLsCmd.Output(); err == nil {
				var files []map[string]interface{}
				if json.Unmarshal(rootLsOutput, &files) == nil && len(files) > 0 {
					if id, ok := files[0]["ID"].(string); ok {
						parentID = id
					} else if id, ok := files[0]["Id"].(string); ok {
						parentID = id
					}
				}
			}
		}

		if parentID == "" {
			parentID = "root"
		}
	} else {
		grandParentPath := path.Dir(parentPath)
		parentName := path.Base(parentPath)

		searchPath := grandParentPath
		if strings.HasSuffix(parentPath, ":") {
			searchPath = parentPath
			parentName = ""
		}

		linkArgsParent := []string{
			"lsjson",
			searchPath,
			"--config", configPath,
			"--dirs-only",
			"--no-modtime",
			"--no-mimetype",
		}

		linkCmdParent := exec.CommandContext(ctx, "rclone", linkArgsParent...)
		if linkOutputParent, err := linkCmdParent.Output(); err == nil {
			var files []map[string]interface{}
			if json.Unmarshal(linkOutputParent, &files) == nil {
				for _, folder := range files {
					if name, ok := folder["Name"].(string); ok && name == parentName {
						if id, ok := folder["ID"].(string); ok {
							parentID = id
						} else if id, ok := folder["Id"].(string); ok {
							parentID = id
						}
						break
					}
				}
			}
		} else {
			slog.Warn("Failed to list grandparent directory", "grandParentPath", grandParentPath, "error", err)
		}
	}

	if fileID != "" && parentID != "" {
		baseURL := strings.TrimRight(r.cfg.IndexURL, "/")
		encodedFileName := url.PathEscape(task.FileName)
		encodedFileName = strings.ReplaceAll(encodedFileName, "%2F", "/")

		task.RemoteURL = fmt.Sprintf("%s/id/folder/%s/file/%s/%s", baseURL, parentID, fileID, encodedFileName)
		slog.Info("Generated ID-based Index URL", "url", task.RemoteURL)
		return true
	}

	slog.Warn("Could not generate ID-based URL (missing IDs)", "fileID", fileID, "parentID", parentID, "parentPath", parentPath)
	return false
}

func (r *RcloneUploader) generatePathBasedIndexURL(task *domain.Task, currentRemotePath string) bool {
	remotePathSlash := strings.ReplaceAll(currentRemotePath, "\\", "/")
	rcloneDestSlash := strings.ReplaceAll(r.cfg.RcloneDest, "\\", "/")
	rcloneDestSlash = strings.TrimRight(rcloneDestSlash, "/")

	var relPath string
	if strings.HasPrefix(remotePathSlash, rcloneDestSlash) {
		relPath = strings.TrimPrefix(remotePathSlash, rcloneDestSlash)
	} else {
		parts := strings.SplitN(remotePathSlash, ":", 2)
		if len(parts) > 1 {
			relPath = parts[1]
		} else {
			relPath = remotePathSlash
		}
	}

	relPath = strings.TrimLeft(relPath, "/")
	pathParts := strings.Split(relPath, "/")
	for i, part := range pathParts {
		pathParts[i] = url.PathEscape(part)
	}
	encodedPath := strings.Join(pathParts, "/")
	encodedPath = strings.ReplaceAll(encodedPath, "%2F", "/")

	baseURL := strings.TrimRight(r.cfg.IndexURL, "/")
	task.RemoteURL = fmt.Sprintf("%s/%s", baseURL, encodedPath)
	slog.Info("Generated Path-based Index URL", "url", task.RemoteURL)
	return true
}

func (r *RcloneUploader) generateDirectLink(ctx context.Context, task *domain.Task, configPath, currentRemotePath string, isDirUpload bool) {
	linkArgs := []string{
		"link",
		"--config", configPath,
		currentRemotePath,
	}

	linkCmd := exec.CommandContext(ctx, "rclone", linkArgs...)
	linkOutput, linkErr := linkCmd.Output()
	if linkErr == nil {
		task.RemoteURL = strings.TrimSpace(string(linkOutput))
		return
	}

	slog.Warn("Failed to get direct link", "fileName", task.FileName, "error", linkErr)

	if !isDirUpload {
		slog.Debug("Skipping directory fallback for file", "fileName", task.FileName)
		return
	}

	r.generateDirectoryLink(ctx, task, configPath, currentRemotePath)
}

func (r *RcloneUploader) generateDirectoryLink(ctx context.Context, task *domain.Task, configPath, currentRemotePath string) {
	parentPath := path.Dir(currentRemotePath)
	idArgs := []string{
		"lsjson",
		"--config", configPath,
		"--dirs-only",
		parentPath,
	}
	idCmd := exec.CommandContext(ctx, "rclone", idArgs...)
	idOutput, idErr := idCmd.Output()
	if idErr != nil {
		slog.Error("Failed to list parent directory contents", "error", idErr)
	} else {
		var files []map[string]interface{}
		if errUnmarshal := json.Unmarshal(idOutput, &files); errUnmarshal != nil {
			slog.Error("Could not parse lsjson output", "error", errUnmarshal)
		} else {
			for _, file := range files {
				if name, ok := file["Name"].(string); ok && name == task.FileName {
					if id, ok := file["ID"].(string); ok {
						task.RemoteURL = "https://drive.google.com/drive/folders/" + id
						return
					}
					if id, ok := file["Id"].(string); ok {
						task.RemoteURL = "https://drive.google.com/drive/folders/" + id
						return
					}
					slog.Warn("Found folder but no ID field in lsjson output", "fileName", task.FileName)
				}
			}
		}
	}

	linkArgsParent := []string{
		"link",
		"--config", configPath,
		parentPath,
	}
	linkCmdParent := exec.CommandContext(ctx, "rclone", linkArgsParent...)
	linkOutputParent, linkErrParent := linkCmdParent.Output()
	if linkErrParent == nil {
		baseURL := strings.TrimSpace(string(linkOutputParent))
		task.RemoteURL = baseURL + "#folders/" + task.FileName
	} else {
		slog.Warn("Failed to get parent directory link", "fileName", task.FileName, "error", linkErrParent)
		task.RemoteURL = "https://drive.google.com/drive/search?q=\"" + task.FileName + "\" in parents"
	}
}

func (r *RcloneUploader) parseRcloneProgress(taskID string, reader io.ReadCloser, onProgress func(ProgressUpdate), onActivity func()) {
	scanner := bufio.NewScanner(reader)
	scanner.Split(utils.ScanLinesWithCR)

	segmentRegex := regexp.MustCompile(`(\d+(?:\.\d+)?)\s*([a-zA-Z]*i?[Bb]?)\s*/\s*(\d+(?:\.\d+)?)\s*([a-zA-Z]*i?[Bb]?),\s*(\d+(?:\.\d+)?)%,\s*(\d+(?:\.\d+)?)\s*([a-zA-Z]*i?[Bb]?)/s,\s*ETA\s+([^\d]*)(\d+[smhd](?:\d+[smhd])*)?`)

	lineCount := 0
	lastUpdate := time.Now()

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		onActivity()

		lineCount++
		if lineCount <= 20 {
			slog.Info("Rclone raw output", "taskID", taskID, "line", line)
		}

		lowerLine := strings.ToLower(line)
		if strings.Contains(lowerLine, "token expired") ||
			strings.Contains(lowerLine, "invalid_grant") ||
			strings.Contains(lowerLine, "unauthorized") ||
			strings.Contains(lowerLine, "token_has_expired") ||
			strings.Contains(lowerLine, "oauth2") && strings.Contains(lowerLine, "error") {
			slog.Error("Rclone auth error detected", "taskID", taskID, "line", line)
			onProgress(ProgressUpdate{Error: "Rclone token expired! Refresh token dengan: docker exec -it zee-mirror-bot rclone config"})
		}

		allMatches := segmentRegex.FindAllStringSubmatch(line, -1)

		if len(allMatches) > 0 {
			for i, matches := range allMatches {
				if len(matches) >= 8 {
					uploadedVal := matches[1] + matches[2]
					totalVal := matches[3] + matches[4]

					upd := ProgressUpdate{
						UploadedSize: utils.ParseBytesString(uploadedVal),
						TotalSize:    utils.ParseBytesString(totalVal),
						Speed:        utils.ParseBytesString(matches[6] + matches[7]),
					}

					if pct, err := strconv.ParseFloat(matches[5], 64); err == nil {
						upd.Progress = pct
					}

					if len(matches) >= 10 && matches[9] != "" {
						if d, err := time.ParseDuration(matches[9]); err == nil {
							upd.ETA = d
						}
					}

					if i%10 == 0 && time.Since(lastUpdate) >= 2*time.Second {
						onProgress(upd)
						lastUpdate = time.Now()
					}
				}
			}
		} else {
			simpleProgressRegex := regexp.MustCompile(`(\d+(?:\.\d+)?)%`)
			simpleSizeRegex := regexp.MustCompile(`(\d+(?:\.\d+)?)\s*([a-zA-Z]*i?[Bb]?)\s*/\s*(\d+(?:\.\d+)?)\s*([a-zA-Z]*i?[Bb]?)`)
			simpleSpeedRegex := regexp.MustCompile(`(\d+(?:\.\d+)?)\s*([a-zA-Z]*i?[Bb]?)/s`)

			upd := ProgressUpdate{}
			found := false

			if allPct := simpleProgressRegex.FindAllStringSubmatch(line, -1); len(allPct) > 0 {
				lastPct := allPct[len(allPct)-1]
				if pct, err := strconv.ParseFloat(lastPct[1], 64); err == nil {
					upd.Progress = pct
					found = true
				}
			}

			if allSize := simpleSizeRegex.FindAllStringSubmatch(line, -1); len(allSize) > 0 {
				lastSize := allSize[len(allSize)-1]
				if len(lastSize) >= 5 {
					upd.UploadedSize = utils.ParseBytesString(lastSize[1] + lastSize[2])
					upd.TotalSize = utils.ParseBytesString(lastSize[3] + lastSize[4])
					found = true
				}
			}

			if allSpeed := simpleSpeedRegex.FindAllStringSubmatch(line, -1); len(allSpeed) > 0 {
				lastSpeed := allSpeed[len(allSpeed)-1]
				if len(lastSpeed) >= 3 {
					upd.Speed = utils.ParseBytesString(lastSpeed[1] + lastSpeed[2])
					found = true
				}
			}

			if found && time.Since(lastUpdate) >= 2*time.Second {
				onProgress(upd)
				lastUpdate = time.Now()
			}
		}
	}
}

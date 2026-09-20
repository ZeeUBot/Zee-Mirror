package torrent

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
	registry.RegisterDownloadEngine("aria2", func(cfg *config.Config) downloader.DownloadEngine {
		return NewAria2Engine(cfg.ConfigDir, cfg.Aria2RPCURL, cfg.Aria2RPCSecret)
	})
}

type Aria2Engine struct {
	RPC       *Aria2RPCClient
	ConfigDir string
}

func NewAria2Engine(configDir string, rpcURL, rpcSecret string) *Aria2Engine {
	return &Aria2Engine{
		ConfigDir: configDir,
		RPC:       NewAria2RPCClient(rpcURL, rpcSecret),
	}
}

func (e *Aria2Engine) Download(ctx context.Context, task *domain.Task, outputDir string, onProgress func(downloader.ProgressUpdate)) error {
	if errDir := os.MkdirAll(outputDir, 0750); errDir != nil {
		return &domain.StorageError{Path: outputDir, Err: errDir}
	}

	options := e.buildAria2Options(task, outputDir)

	cleanURL := task.URL
	selection := ""
	if idx := strings.LastIndex(task.URL, "#select="); idx != -1 {
		cleanURL = task.URL[:idx]
		selection = task.URL[idx+len("#select="):]
		options["select-file"] = selection
		slog.Info("Aria2 selective download enabled", "url", cleanURL, "files", selection)
	}

	var gid string
	var err error

	if strings.HasPrefix(cleanURL, "file://") && strings.HasSuffix(strings.ToLower(cleanURL), ".torrent") {
		filePath := strings.TrimPrefix(cleanURL, "file://")
		slog.Info("Loading local torrent file for aria2", "path", filePath)

		content, readErr := os.ReadFile(filePath)
		if readErr != nil {
			return &domain.StorageError{Path: filePath, Err: fmt.Errorf("failed to read local torrent file: %v", readErr)}
		}

		encoded := base64.StdEncoding.EncodeToString(content)
		gid, err = e.RPC.AddTorrent(encoded, options)
	} else {
		gid, err = e.RPC.AddURI(cleanURL, options)
	}

	if err != nil {
		return &domain.NetworkError{URL: task.URL, Err: fmt.Errorf("aria2 rpc add failed: %v", err)}
	}

	task.GID = gid

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = e.RPC.Remove(gid)
			return ctx.Err()
		case <-ticker.C:
			status, err := e.RPC.TellStatus(gid)
			if err != nil {
				slog.Error("Failed to tellStatus from aria2 rpc", "gid", gid, "error", err)
				continue
			}

			if status.Status == "complete" {
				if len(status.FollowedBy) > 0 {
					slog.Info("Aria2 download followed by new gid (likely metadata -> torrent)", "old_gid", gid, "new_gid", status.FollowedBy[0])
					gid = status.FollowedBy[0]
					task.GID = gid
					continue
				}
				finalUpdate := downloader.ProgressUpdate{
					Downloaded: utils.ParseBytesString(status.TotalLength),
					Total:      utils.ParseBytesString(status.TotalLength),
					Progress:   100,
					Speed:      0,
					ETA:        0,
				}
				onProgress(finalUpdate)
				return nil
			}

			if status.Status == "error" {
				return &domain.ExternalError{Tool: "aria2", Err: fmt.Errorf("%s", status.ErrorMessage)}
			}

			if status.Status == "removed" {
				return &domain.ExternalError{Tool: "aria2", Err: fmt.Errorf("task was removed")}
			}

			update := downloader.ProgressUpdate{}
			update.Downloaded = utils.ParseBytesString(status.CompletedLength)
			update.Total = utils.ParseBytesString(status.TotalLength)
			update.Speed = utils.ParseBytesString(status.DownloadSpeed)
			if update.Total > 0 {
				update.Progress = float64(update.Downloaded) / float64(update.Total) * 100
				if update.Speed > 0 {
					update.ETA = time.Duration(float64(update.Total-update.Downloaded)/float64(update.Speed)) * time.Second
				}
			}
			update.Connections = utils.ParseInt(status.Connections)

			onProgress(update)
		}
	}
}

func (e *Aria2Engine) buildAria2Options(task *domain.Task, outputDir string) map[string]interface{} {
	connections := "4"
	split := "4"

	noRange := downloader.IsNonRangeURL(task.URL)
	if noRange {
		slog.Info("URL detected as non-range-request compatible, using single connection", "url", task.URL)
		connections = "1"
		split = "1"
	} else if task.TotalSize > 0 {
		switch {
		case task.TotalSize < 50*1024*1024:
			connections = "4"
			split = "4"
		case task.TotalSize < 500*1024*1024:
			connections = "8"
			split = "16"
		case task.TotalSize < 2*1024*1024*1024:
			connections = "16"
			split = "32"
		default:
			connections = "16"
			split = "64"
		}
	}

	// For non-range URLs, send a plain GET: resume probing would otherwise
	// fail with errorCode=8 when the server replies with the full body.
	resume := strconv.FormatBool(!noRange)

	options := map[string]interface{}{
		"dir":                              outputDir,
		"allow-overwrite":                  "true",
		"continue":                         resume,
		"always-resume":                    resume,
		"max-connection-per-server":        connections,
		"split":                            split,
		"min-split-size":                   "1M",
		"max-overall-download-limit":       "0",
		"max-resume-failure-tries":         "0",
		"retry-wait":                       "5",
		"connect-timeout":                  "60",
		"timeout":                          "60",
		"user-agent":                       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		"async-dns":                        "true",
		"file-allocation":                  "none",
		"disk-cache":                       "128M",
		"enable-mmap":                      "true",
		"check-certificate":                "false",
		"optimize-concurrent-downloads":    "true",
		"max-file-not-found":               "10",
		"disable-ipv6":                     "true",
		"enable-http-pipelining":           "true",
		"content-disposition-default-utf8": "true",
		"remote-time":                      "true",
		"check-integrity":                  resume,
	}

	cookiesPath := filepath.Join(e.ConfigDir, "cookies.txt")
	if _, err := os.Stat(cookiesPath); err == nil {
		options["load-cookies"] = cookiesPath
	}

	if task.FileName != "" && task.FileName != "unknown_file" && !utils.IsGenericName(task.FileName) {
		options["out"] = task.FileName
	}

	if utils.IsMagnetLink(task.URL) {
		options["seed-time"] = "0"
	}

	if task.Type == domain.TypeTorrent {
		options["seed-time"] = "0"
	}

	return options
}

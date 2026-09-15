package mega

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"zee-mirror/internal/config"
	"zee-mirror/internal/domain"
	"zee-mirror/internal/downloader"
	"zee-mirror/plugins/registry"
)

func init() {
	registry.RegisterDownloadEngine("mega", func(cfg *config.Config) downloader.DownloadEngine {
		return NewEngine(cfg)
	})
}

type Engine struct {
	Config *config.Config
}

func NewEngine(cfg *config.Config) *Engine {
	return &Engine{Config: cfg}
}

// CanHandle claims Mega links.
func (e *Engine) CanHandle(url string) bool {
	return strings.Contains(url, "mega.nz")
}

func (e *Engine) Download(ctx context.Context, task *domain.Task, outputDir string, onProgress func(downloader.ProgressUpdate)) error {
	var cmdName string
	if _, err := exec.LookPath("mega-get"); err == nil {
		cmdName = "mega-get"
	} else if _, err := exec.LookPath("megadl"); err == nil {
		cmdName = "megadl"
	} else {
		return fmt.Errorf("mega-get atau megadl tidak ditemukan di server. Silakan install MEGAcmd atau megatools di Dockerfile")
	}

	args := []string{}
	if cmdName == "megadl" {
		args = append(args, "--path", outputDir, task.URL)
	} else {
		args = append(args, task.URL, outputDir)
	}

	cmd := execCommand(ctx, cmdName, args...)

	onProgress(downloader.ProgressUpdate{
		Message: "Mengunduh file dari Mega (Progress tidak tersedia di mode CLI)...",
	})

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gagal mengunduh dari Mega: %v, output: %s", err, string(output))
	}

	task.Mu.Lock()
	if task.FileName == "" || task.FileName == "unknown_file" {
		task.FileName = "mega_downloaded_file"
	}
	task.Mu.Unlock()

	return nil
}

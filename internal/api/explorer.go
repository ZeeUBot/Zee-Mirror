package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func (s *Server) handleExplorer(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	fullPath := filepath.Join(s.Service.Config.DownloadDir, path)

	cleanBase, _ := filepath.Abs(s.Service.Config.DownloadDir)
	cleanTarget, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(cleanTarget, cleanBase) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	if r.Method == http.MethodDelete {
		if path == "" || path == "/" || path == "." {
			http.Error(w, "Cannot delete root directory", http.StatusBadRequest)
			return
		}
		// #nosec G703 -- fullPath containment-checked against DownloadDir above
		if err := os.RemoveAll(fullPath); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	files, err := os.ReadDir(fullPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var result []map[string]interface{}
	for _, f := range files {
		info, _ := f.Info()
		name := f.Name()

		if s.shouldSkipFile(path, name) {
			continue
		}

		displayName, status := s.resolveFileStatus(f, name, fullPath)
		if status == "ignore" {
			continue
		}

		result = append(result, map[string]interface{}{
			"name":        name,
			"displayName": displayName,
			"isDir":       f.IsDir(),
			"size":        info.Size(),
			"time":        info.ModTime(),
			"status":      status,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		slog.Error("Failed to encode explorer response", "error", err)
	}
}

func (s *Server) shouldSkipFile(path, name string) bool {
	if path == "" {
		if strings.Contains(name, ":") || len(name) > 40 {
			return true
		}
		if strings.HasSuffix(name, ".binlog") || name == "tqueue" || name == "webhooks" || name == "webhooks_db" {
			return true
		}
	}
	return false
}

func (s *Server) resolveFileStatus(f os.DirEntry, name, fullPath string) (string, string) {
	displayName := name
	status := "folder"
	if !f.IsDir() {
		status = "file"
	}

	if f.IsDir() && (len(name) == 8 || strings.HasPrefix(name, "batch_")) {
		if task := s.Service.TaskManager.GetTask(name); task != nil {
			return task.FileName, "active"
		}

		ctx := context.Background()
		if tr, err := s.Service.DB.GetTaskByID(ctx, name); err == nil && tr != nil {
			return tr.FileName, "finished"
		}

		subPath := filepath.Join(fullPath, name)
		if subFiles, errDir := os.ReadDir(subPath); errDir == nil && len(subFiles) > 0 {
			return subFiles[0].Name(), "orphan"
		}
		return "", "ignore"
	}
	return displayName, status
}

func (s *Server) handleRemoteExplorer(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	remotePath := s.Service.Config.RcloneDest
	if path != "" {
		remotePath = filepath.Join(remotePath, path)
	}
	remotePath = filepath.ToSlash(remotePath)

	configPath := filepath.Join(s.Service.Config.ConfigDir, "rclone.conf")

	if r.Method == http.MethodDelete {
		if path == "" || path == "/" || path == "." {
			http.Error(w, "Cannot delete root remote directory", http.StatusBadRequest)
			return
		}
		// #nosec G702 -- remotePath anchored to configured RcloneDest; rclone runs without shell
		cmd := exec.CommandContext(r.Context(), "rclone", "purge", remotePath, "--config", configPath)
		if purgeErr := cmd.Run(); purgeErr != nil {
			// #nosec G702 -- remotePath anchored to configured RcloneDest; rclone runs without shell
			cmd = exec.CommandContext(r.Context(), "rclone", "deletefile", remotePath, "--config", configPath)
			if deleteErr := cmd.Run(); deleteErr != nil {
				http.Error(w, fmt.Sprintf("Delete failed: %v", deleteErr), http.StatusInternalServerError)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	// #nosec G702 -- remotePath anchored to configured RcloneDest; rclone runs without shell
	cmd := exec.CommandContext(r.Context(), "rclone", "lsjson", remotePath, "--fast-list", "--config", configPath)
	output, err := cmd.Output()
	if err != nil {
		http.Error(w, fmt.Sprintf("Rclone failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	// #nosec G705 -- payload is rclone lsjson JSON served with an explicit application/json header
	_, _ = w.Write(output)
}

func (s *Server) handleRemoteLink(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	remotePath := s.Service.Config.RcloneDest
	if path != "" {
		remotePath = filepath.Join(remotePath, path)
	}
	remotePath = filepath.ToSlash(remotePath)
	configPath := filepath.Join(s.Service.Config.ConfigDir, "rclone.conf")

	// #nosec G702 -- remotePath anchored to configured RcloneDest; rclone runs without shell
	cmd := exec.CommandContext(r.Context(), "rclone", "link", remotePath, "--config", configPath)
	output, err := cmd.Output()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get link: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"link": strings.TrimSpace(string(output))})
}

func (s *Server) handleWipeOrphans(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	baseDir := s.Service.Config.DownloadDir
	files, err := os.ReadDir(baseDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	count := 0
	for _, f := range files {
		name := f.Name()
		if f.IsDir() && (len(name) == 8 || strings.HasPrefix(name, "batch_")) {
			if task := s.Service.TaskManager.GetTask(name); task == nil {
				_ = os.RemoveAll(filepath.Join(baseDir, name))
				count++
			}
		}
	}

	if err := json.NewEncoder(w).Encode(map[string]interface{}{"wiped": count}); err != nil {
		slog.Error("Failed to encode wipe orphans response", "error", err)
	}
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseMultipartForm(500 << 20)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	file, handler, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Error retrieving the file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	path := r.FormValue("path")
	path = filepath.Clean(path)
	if strings.Contains(path, "..") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	destDir := filepath.Join(s.Service.Config.DownloadDir, path)
	_ = os.MkdirAll(destDir, 0750)

	cleanBase, _ := filepath.Abs(s.Service.Config.DownloadDir)
	cleanDestDir, _ := filepath.Abs(destDir)
	if !strings.HasPrefix(cleanDestDir, cleanBase) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	safeFilename := filepath.Base(handler.Filename)
	if safeFilename == "." || safeFilename == "/" || safeFilename == ".." {
		http.Error(w, "Invalid filename", http.StatusBadRequest)
		return
	}
	destPath := filepath.Join(destDir, safeFilename)
	dst, err := os.Create(destPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	_, err = io.Copy(dst, file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	slog.Info("File uploaded from dashboard", "name", handler.Filename, "size", handler.Size, "dest", destPath)

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "Upload successful", "file": handler.Filename}); err != nil {
		slog.Error("Failed to encode upload response", "error", err)
	}
}

var previewMimeTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
	".bmp":  "image/bmp",
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".mkv":  "video/x-matroska",
	".pdf":  "application/pdf",
	".txt":  "text/plain; charset=utf-8",
	".log":  "text/plain; charset=utf-8",
	".json": "application/json",
	".csv":  "text/csv; charset=utf-8",
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "Path required", http.StatusBadRequest)
		return
	}

	fullPath := filepath.Join(s.Service.Config.DownloadDir, path)

	cleanBase, _ := filepath.Abs(s.Service.Config.DownloadDir)
	cleanTarget, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(cleanTarget, cleanBase) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// #nosec G703 -- fullPath containment-checked against DownloadDir above
	file, err := os.Open(fullPath)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil || stat.IsDir() {
		http.Error(w, "Invalid file", http.StatusBadRequest)
		return
	}

	ext := strings.ToLower(filepath.Ext(fullPath))
	mimeType, ok := previewMimeTypes[ext]
	if !ok {
		mimeType = "application/octet-stream"
	}

	buf := make([]byte, 512)
	_, _ = file.Read(buf)
	_, _ = file.Seek(0, io.SeekStart)

	detectedMime := http.DetectContentType(buf)
	if detectedMime != "application/octet-stream" {
		mimeType = detectedMime
	}

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), file)
}

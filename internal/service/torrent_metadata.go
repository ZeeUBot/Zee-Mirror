package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"zee-mirror/internal/domain"
)

func (s *BotService) addSessionStatus(sessionID, message string) {
	s.TaskManager.Mu.Lock()
	defer s.TaskManager.Mu.Unlock()
	if session, exists := s.TaskManager.TorrentSessions[sessionID]; exists {
		timestamp := time.Now().Format("15:04:05")
		session.StatusMessages = append(session.StatusMessages, fmt.Sprintf("[%s] %s", timestamp, message))
		if len(session.StatusMessages) > 10 {
			session.StatusMessages = session.StatusMessages[len(session.StatusMessages)-10:]
		}
	}
}

func (s *BotService) FetchTorrentMetadataBackground(sessionID string) {
	s.TaskManager.Mu.RLock()
	session, exists := s.TaskManager.TorrentSessions[sessionID]
	s.TaskManager.Mu.RUnlock()

	if !exists {
		return
	}

	s.TaskManager.Mu.Lock()
	session.IsFetching = true
	session.StatusMessages = []string{}
	s.TaskManager.Mu.Unlock()

	s.addSessionStatus(sessionID, "📡 Memulai pencarian metadata...")
	slog.Info("Starting background metadata fetch", "sessionID", sessionID, "url", session.URL)

	metaFilePath, tmpDir, err := s.getMetadataFilePath(sessionID, session.URL)
	if err != nil {
		s.updateSessionError(sessionID, err.Error())
		return
	}
	if tmpDir != "" {
		defer os.RemoveAll(tmpDir)
	}

	var files []domain.TorrentFile
	if metaFilePath != "" {
		s.addSessionStatus(sessionID, "📋 Memparsing daftar file...")
		files = s.parseTorrentMetadataFile(sessionID, metaFilePath)
	}

	s.finalizeMetadataFetch(sessionID, files)
}

func (s *BotService) getMetadataFilePath(sessionID, sessionURL string) (string, string, error) {
	if strings.HasPrefix(sessionURL, "magnet:") {
		tmpDir, err := os.MkdirTemp("", "torrent_meta_"+sessionID+"_")
		if err != nil {
			s.addSessionStatus(sessionID, "❌ Gagal membuat folder temporary.")
			return "", "", fmt.Errorf("%w: failed to create temp dir: %v", domain.ErrStorage, err)
		}

		path, err := s.fetchMagnetMetadata(sessionID, sessionURL, tmpDir)
		if err != nil {
			os.RemoveAll(tmpDir)
			return "", "", err
		}
		return path, tmpDir, nil
	}

	s.addSessionStatus(sessionID, "📄 Memproses file .torrent lokal...")
	urlStr := strings.TrimPrefix(sessionURL, "file://")
	slog.Info("Using local torrent file", "sessionID", sessionID, "path", urlStr)
	return urlStr, "", nil
}

func (s *BotService) fetchMagnetMetadata(sessionID, urlStr, tmpDir string) (string, error) {
	s.addSessionStatus(sessionID, "🧲 Menghubungkan ke peer (Magnet Link)...")

	fetchCtx, fetchCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer fetchCancel()

	args := []string{
		"--dir=" + tmpDir,
		"--seed-time=0",
		"--bt-stop-timeout=60",
		"--bt-save-metadata=true",
		"--bt-metadata-only=true",
		"--enable-dht=true",
		"--enable-peer-exchange=true",
		"--dht-entry-point=dht.transmissionbt.com:6881",
		"--dht-entry-point=router.bittorrent.com:6881",
		"--summary-interval=10",
		urlStr,
	}

	cmd := execCommand(fetchCtx, "aria2c", args...)
	outputBytes, _ := cmd.CombinedOutput()
	output := stripANSI(string(outputBytes))

	metaFiles, _ := filepath.Glob(filepath.Join(tmpDir, "*.torrent"))
	if len(metaFiles) > 0 {
		metaFilePath := metaFiles[0]
		s.addSessionStatus(sessionID, "✅ Metadata berhasil didapatkan.")
		slog.Info("Found downloaded metadata file", "sessionID", sessionID, "path", metaFilePath)
		return metaFilePath, nil
	}

	s.addSessionStatus(sessionID, "❌ Gagal mendapatkan file metadata .torrent.")
	slog.Warn("No .torrent file found after aria2c run", "sessionID", sessionID, "output", output)
	return "", fmt.Errorf("%w: metadata fetch failed", domain.ErrExternal)
}

func (s *BotService) finalizeMetadataFetch(sessionID string, files []domain.TorrentFile) {
	s.TaskManager.Mu.Lock()
	defer s.TaskManager.Mu.Unlock()

	session, exists := s.TaskManager.TorrentSessions[sessionID]
	if !exists {
		return
	}

	session.IsFetching = false
	if len(files) > 0 {
		session.Files = files
		session.Error = ""
		slog.Info("Successfully fetched torrent metadata", "sessionID", sessionID, "files", len(files))

		if session.FileName == "" {
			session.FileName = s.determineTorrentName(files)
		}
	} else {
		session.Files = nil
		session.Error = "Gagal mengambil daftar file. Metadata mungkin korup atau tidak terbaca."
		slog.Warn("Failed to parse torrent metadata", "sessionID", sessionID)
	}
}

func (s *BotService) determineTorrentName(files []domain.TorrentFile) string {
	commonRoot := ""
	for _, f := range files {
		path := f.Path
		for strings.HasPrefix(path, "./") || strings.HasPrefix(path, "/") {
			path = strings.TrimPrefix(path, "./")
			path = strings.TrimPrefix(path, "/")
		}
		parts := strings.Split(filepath.ToSlash(path), "/")
		if len(parts) > 1 {
			root := parts[0]
			if root == "." || root == ".." {
				if len(parts) > 2 {
					root = parts[1]
				} else {
					continue
				}
			}

			if commonRoot == "" {
				commonRoot = root
			} else if commonRoot != root {
				commonRoot = ""
				break
			}
		} else if len(files) > 1 {
			commonRoot = ""
			break
		}
	}

	if commonRoot != "" && commonRoot != "." {
		return commonRoot
	} else if len(files) == 1 {
		return files[0].Name
	}
	return ""
}

func (s *BotService) updateSessionError(sessionID, errMsg string) {
	s.TaskManager.Mu.Lock()
	defer s.TaskManager.Mu.Unlock()
	if session, exists := s.TaskManager.TorrentSessions[sessionID]; exists {
		session.IsFetching = false
		session.Error = errMsg
	}
}

func (s *BotService) parseTorrentMetadataFile(sessionID, torrentPath string) []domain.TorrentFile {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	slog.Info("Parsing torrent file", "path", torrentPath)

	if _, err := os.Stat(torrentPath); os.IsNotExist(err) {
		slog.Error("Torrent file does not exist", "path", torrentPath)
		s.addSessionStatus(sessionID, "❌ File metadata tidak ditemukan di sistem.")
		return nil
	}

	cmd := execCommand(ctx, "aria2c", "--show-files=true", torrentPath)
	outputBytes, err := cmd.CombinedOutput()
	cleanOutput := stripANSI(string(outputBytes))

	if err != nil {
		slog.Error("aria2c --show-files failed", "path", torrentPath, "error", err, "output", cleanOutput)
		s.addSessionStatus(sessionID, "❌ Perintah show-files gagal dijalankan.")
		return nil
	}

	files := parseTorrentOutput(cleanOutput)
	if len(files) == 0 {
		slog.Warn("No files parsed from output", "output", cleanOutput)
		s.addSessionStatus(sessionID, "⚠️ Daftar file terbaca kosong dari metadata.")

		slog.Info("Raw aria2c output for debugging", "output", cleanOutput)
	}

	return files
}

func stripANSI(s string) string {
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	return ansiRegex.ReplaceAllString(s, "")
}

func parseTorrentOutput(output string) []domain.TorrentFile {
	var files []domain.TorrentFile
	lines := strings.Split(output, "\n")

	headerRegex := regexp.MustCompile(`^\s*(\d+)\|(.+)`)
	sizeRegex := regexp.MustCompile(`^\s*\|.*?\(([\d,]+)\)$`)

	pipePattern := regexp.MustCompile(`^\s*(\d+)\s*\|\s*(.+?)\s*\|\s*(\d+)\s*\|`)
	spacePattern := regexp.MustCompile(`^\s*(\d+)\s+(.+?)\s+(\d+)\s+(?:true|false)`)

	var currentIdx int
	var currentPath string
	var waitingForSize bool

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "===") || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "idx|") {
			continue
		}

		if matches := pipePattern.FindStringSubmatch(line); len(matches) >= 4 {
			idx, _ := strconv.Atoi(matches[1])
			size, _ := strconv.ParseInt(matches[3], 10, 64)
			path := matches[2]
			name := filepath.Base(path)

			files = append(files, domain.TorrentFile{
				Index: idx,
				Name:  name,
				Path:  path,
				Size:  size,
			})
			waitingForSize = false
			continue
		}

		if matches := spacePattern.FindStringSubmatch(line); len(matches) >= 4 {
			idx, _ := strconv.Atoi(matches[1])
			size, _ := strconv.ParseInt(matches[3], 10, 64)
			path := matches[2]
			name := filepath.Base(path)

			files = append(files, domain.TorrentFile{
				Index: idx,
				Name:  name,
				Path:  path,
				Size:  size,
			})
			waitingForSize = false
			continue
		}

		if waitingForSize {
			if matches := sizeRegex.FindStringSubmatch(line); len(matches) >= 2 {
				sizeStr := strings.ReplaceAll(matches[1], ",", "")
				size, _ := strconv.ParseInt(sizeStr, 10, 64)

				name := filepath.Base(currentPath)
				files = append(files, domain.TorrentFile{
					Index: currentIdx,
					Name:  name,
					Path:  currentPath,
					Size:  size,
				})
				waitingForSize = false
				continue
			}
		}

		if matches := headerRegex.FindStringSubmatch(line); len(matches) >= 3 {
			idx, _ := strconv.Atoi(matches[1])
			path := strings.TrimSpace(matches[2])
			currentIdx = idx
			currentPath = path
			waitingForSize = true
			continue
		}
	}

	return files
}

package utils

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func ProgressBar(progress float64, width int) string {
	if width <= 0 {
		width = 10
	}

	filled := int(progress / 100 * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}

	empty := width - filled

	filledChar := "■"
	emptyChar := "□"

	bar := strings.Repeat(filledChar, filled) + strings.Repeat(emptyChar, empty)
	return fmt.Sprintf("[%s] %.1f%%", bar, progress)
}

func FormatBytes(bytes int64) string {
	if bytes < 0 {
		return "0 B"
	}
	return FormatBytesUint64(uint64(bytes))
}

func FormatBytesUint64(bytes uint64) string {
	const (
		KB uint64 = 1024
		MB        = KB * 1024
		GB        = MB * 1024
		TB        = GB * 1024
	)

	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.2f TB", float64(bytes)/float64(TB))
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func FormatSpeed(bytesPerSec int64) string {
	return FormatBytes(bytesPerSec) + "/s"
}

func FormatDuration(d time.Duration) string {
	if d < 0 {
		return "∞"
	}

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

func EscapeMarkdownV2(text string) string {
	reserved := "_*[]()~`>#+-=|{}.!\\"
	var result strings.Builder
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		char := runes[i]

		if char == '\\' && i+1 < len(runes) {
			next := runes[i+1]
			if strings.ContainsRune(reserved, next) {
				result.WriteRune('\\')
				result.WriteRune(next)
				i++
				continue
			}
		}

		if strings.ContainsRune(reserved, char) {
			result.WriteRune('\\')
			result.WriteRune(char)
		} else {
			result.WriteRune(char)
		}
	}
	return result.String()
}

func EscapeMarkdownV2Code(text string) string {
	replacer := strings.NewReplacer(
		"`", "\\`",
		"\\", "\\\\",
	)
	return replacer.Replace(text)
}

func SanitizeFileName(filename string) string {
	reg := regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
	filename = reg.ReplaceAllString(filename, "_")

	if len(filename) > 200 {
		ext := filepath.Ext(filename)
		name := strings.TrimSuffix(filename, ext)
		if len(name) > 200-len(ext) {
			name = name[:200-len(ext)]
		}
		filename = name + ext
	}

	return filename
}

func ExtractURLFromText(text string) string {
	urlRegex := regexp.MustCompile(`https?://[^\s<>'"{}|\\^` + "`" + `\[\]]+`)
	matches := urlRegex.FindStringSubmatch(text)
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}

func ExtractMagnetFromText(text string) string {
	magnetRegex := regexp.MustCompile(`magnet:\?xt=urn:[^\s]+`)
	matches := magnetRegex.FindStringSubmatch(text)
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}

func ParseFlags(args string) (url string, zip bool, unzip bool, password string, quality string, name string, subs string, hardsub bool) {
	parts := strings.Fields(args)

	for i := 0; i < len(parts); i++ {
		part := parts[i]
		switch part {
		case "-z":
			zip = true
		case "-uz":
			unzip = true
		case "-p":
			if i+1 < len(parts) {
				password = parts[i+1]
				i++
			}
		case "-q":
			if i+1 < len(parts) {
				quality = parts[i+1]
				i++
			}
		case "-s", "-subs":
			if i+1 < len(parts) {
				subs = parts[i+1]
				i++
			}
		case "-hs", "-hardsub":
			hardsub = true
		case "-n", "-name":
			var extracted string
			extracted, i = parseNameArg(parts, i)
			if extracted != "" {
				name = extracted
			}
		default:
			if url == "" && (strings.HasPrefix(part, "http") || strings.HasPrefix(part, "magnet:")) {
				url = part
			}
		}
	}

	return
}

func ExtractDest(args string) string {
	parts := strings.Fields(args)
	for i := 0; i < len(parts); i++ {
		part := parts[i]
		switch part {
		case "-z", "-uz", "-hs", "-hardsub":
		case "-p", "-q", "-s", "-subs":
			i++
		case "-n", "-name":
			for i+1 < len(parts) && !strings.HasPrefix(parts[i+1], "-") && !strings.HasPrefix(parts[i+1], "http") && !strings.HasPrefix(parts[i+1], "magnet:") {
				i++
			}
		default:
			if strings.HasPrefix(part, "http") || strings.HasPrefix(part, "magnet:") {
				continue
			}
			return part
		}
	}
	return ""
}

func parseNameArg(parts []string, currentIndex int) (string, int) {
	var nameParts []string
	i := currentIndex
	for i+1 < len(parts) {
		nextPart := parts[i+1]
		if strings.HasPrefix(nextPart, "-") {
			break
		}
		if strings.HasPrefix(nextPart, "http") || strings.HasPrefix(nextPart, "magnet:") {
			break
		}

		nameParts = append(nameParts, nextPart)
		i++
	}
	return strings.Join(nameParts, " "), i
}

func IsValidURL(s string) bool {
	urlRegex := regexp.MustCompile(`^(https?|ftp|magnet):`)
	return urlRegex.MatchString(s)
}

func IsMagnetLink(s string) bool {
	return strings.HasPrefix(s, "magnet:?")
}

func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-1] + "…"
}

func StatusEmoji(status string) string {
	switch status {
	case "queued":
		return "⏳"
	case "downloading":
		return "📥"
	case "fetching":
		return "🌩️"
	case "extracting":
		return "📂"
	case "zipping":
		return "🗜️"
	case "uploading":
		return "📤"
	case "completed":
		return "✅"
	case "failed":
		return "❌"
	case "cancelled":
		return "🚫"
	default:
		return "❓"
	}
}

func FormatStatus(status string) string {
	if status == "" {
		return ""
	}
	return strings.ToUpper(status[:1]) + status[1:]
}

func isIgnoredFileName(name string) bool {
	lowerName := strings.ToLower(name)
	ignoredNames := map[string]bool{
		"playlist.m3u8": true,
		"index.m3u8":    true,
		"master.m3u8":   true,
		"index.html":    true,
		"manifest.mpd":  true,
		"playlist.mpd":  true,
		"video.mp4":     true,
		"stream.m3u8":   true,
		"watch":         true,
		"video":         true,
		"embed":         true,
		"play":          true,
	}
	return ignoredNames[lowerName]
}

func getFileNameFromQuery(u *url.URL) string {
	q := u.Query()

	if rcd := q.Get("response-content-disposition"); rcd != "" {
		if _, params, err := mime.ParseMediaType(rcd); err == nil {
			if filename, ok := params["filename"]; ok && filename != "" {
				return SanitizeFileName(filename)
			}
		}
	}

	keys := []string{"filename", "file_name", "dn", "file", "zipname"}
	for _, key := range keys {
		if val := q.Get(key); val != "" {
			return SanitizeFileName(val)
		}
	}

	return ""
}

func getFileNameFromPixelDrain(u *url.URL) string {
	if strings.Contains(u.Host, "pixeldrain.com") && strings.HasPrefix(u.Path, "/api/file/") {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 4 {
			return SanitizeFileName(parts[3])
		}
	}
	return ""
}

func GetFileNameFromURL(urlStr string) string {
	if strings.HasPrefix(urlStr, "magnet:?") {
		return getMagnetFileName(urlStr)
	}

	u, err := url.Parse(urlStr)
	if err == nil {
		if name := getFileNameFromQuery(u); name != "" {
			return name
		}

		if name := getFileNameFromPixelDrain(u); name != "" {
			return name
		}

		if name := getPathFileName(u.Path); name != "" {
			return name
		}
	}

	return getFallbackFileName(urlStr)
}

func getMagnetFileName(urlStr string) string {
	if u, err := url.Parse(strings.Replace(urlStr, "magnet:?", "http://localhost/?", 1)); err == nil {
		if name := getFileNameFromQuery(u); name != "" {
			return name
		}
	}
	if idx := strings.Index(urlStr, "dn="); idx != -1 {
		name := urlStr[idx+3:]
		if endIdx := strings.Index(name, "&"); endIdx != -1 {
			name = name[:endIdx]
		}
		if unescaped, err := url.QueryUnescape(name); err == nil {
			return SanitizeFileName(unescaped)
		}
	}
	return "unknown_magnet"
}

func getPathFileName(path string) string {
	if path == "" || path == "/" {
		return ""
	}
	parts := strings.Split(path, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			name := parts[i]
			if i > 0 && isIgnoredFileName(name) {
				continue
			}
			return SanitizeFileName(name)
		}
	}
	return ""
}

func getFallbackFileName(urlStr string) string {
	if idx := strings.Index(urlStr, "?"); idx != -1 {
		urlStr = urlStr[:idx]
	}

	parts := strings.Split(urlStr, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			name := parts[i]
			if !strings.Contains(name, ":") && !strings.Contains(name, "?") {
				return SanitizeFileName(name)
			}
		}
	}

	return "unknown_file"
}

func ParseBytesString(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}

	mult := int64(1)
	suffixes := map[string]int64{
		"kib": 1024,
		"mib": 1024 * 1024,
		"gib": 1024 * 1024 * 1024,
		"tib": 1024 * 1024 * 1024 * 1024,
		"ki":  1024,
		"mi":  1024 * 1024,
		"gi":  1024 * 1024 * 1024,
		"ti":  1024 * 1024 * 1024 * 1024,
		"kb":  1000,
		"mb":  1000 * 1000,
		"gb":  1000 * 1000 * 1000,
		"tb":  1000 * 1000 * 1000 * 1000,
		"k":   1000,
		"m":   1000 * 1000,
		"g":   1000 * 1000 * 1000,
		"t":   1000 * 1000 * 1000 * 1000,
		"b":   1,
	}

	lowerS := strings.ToLower(s)
	lowerS = strings.TrimSuffix(lowerS, "/s")
	lowerS = strings.TrimSuffix(lowerS, "/sec")
	if strings.HasSuffix(lowerS, "b/s") {
		lowerS = strings.TrimSuffix(lowerS, "b/s")
		lowerS += "b"
	} else if strings.HasSuffix(lowerS, "bps") {
		lowerS = strings.TrimSuffix(lowerS, "bps")
	}
	lowerS = strings.TrimSpace(lowerS)

	suffixList := []string{"kib", "mib", "gib", "tib", "ki", "mi", "gi", "ti", "kb", "mb", "gb", "tb", "k", "m", "g", "t", "b"}

	for _, suffix := range suffixList {
		if strings.HasSuffix(lowerS, suffix) {
			mult = suffixes[suffix]
			lowerS = strings.TrimSuffix(lowerS, suffix)
			s = strings.TrimSpace(lowerS)
			break
		}
	}

	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}

	return int64(val * float64(mult))
}

func ParseInt(s string) int {
	val, _ := strconv.Atoi(s)
	return val
}

func CalculateDirSize(path string) (int64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return err
	})
	return size, err
}

func GetLastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) <= n {
		return s
	}

	return strings.Join(lines[len(lines)-n:], "\n")
}

func ResolveFileName(urlStr string) string {
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			return nil
		},
	}

	req, err := http.NewRequest("HEAD", urlStr, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")

	resp, err := client.Do(req)
	if err != nil {
		req.Method = "GET"
		resp, err = client.Do(req)
		if err != nil {
			return ""
		}
	}
	defer func() { _ = resp.Body.Close() }()

	cd := resp.Header.Get("Content-Disposition")
	if cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if filename, ok := params["filename"]; ok && filename != "" {
				return SanitizeFileName(filename)
			}
		}
	}

	if resp.Request.URL.String() != urlStr {
		return GetFileNameFromURL(resp.Request.URL.String())
	}

	return ""
}
func ScanLinesWithCR(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' || data[i] == '\r' {
			return i + 1, data[0:i], nil
		}
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, in)
	return err
}

func DownloadFile(url, dst string) error {
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, resp.Body)
	return err
}

func BoolToEmoji(b bool) string {
	if b {
		return "✅"
	}
	return "❌"
}

func IsGenericName(name string) bool {
	uuidRegex := regexp.MustCompile(`^[a-fA-F0-9]{8}(-[a-fA-F0-9]{4}){3}-[a-fA-F0-9]{12}$`)
	if uuidRegex.MatchString(name) {
		return true
	}
	hexRegex := regexp.MustCompile(`^[a-fA-F0-9]{16,}$`)
	return hexRegex.MatchString(name)
}

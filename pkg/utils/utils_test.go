package utils

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		expected string
		input    int64
	}{
		{"100 B", 100},
		{"1.00 KB", 1024},
		{"1.00 MB", 1048576},
		{"1.00 GB", 1073741824},
	}

	for _, test := range tests {
		result := FormatBytes(test.input)
		if result != test.expected {
			t.Errorf("FormatBytes(%d) = %s; want %s", test.input, result, test.expected)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		expected string
		input    time.Duration
	}{
		{"10s", 10 * time.Second},
		{"1m 5s", 65 * time.Second},
		{"1h 1m 5s", 3665 * time.Second},
	}

	for _, test := range tests {
		result := FormatDuration(test.input)
		if result != test.expected {
			t.Errorf("FormatDuration(%v) = %s; want %s", test.input, result, test.expected)
		}
	}
}

func TestSanitizeFileName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"file.txt", "file.txt"},
		{"file/with/slash.txt", "file_with_slash.txt"},
		{"file*with*star.txt", "file_with_star.txt"},
	}

	for _, test := range tests {
		result := SanitizeFileName(test.input)
		if result != test.expected {
			t.Errorf("SanitizeFileName(%s) = %s; want %s", test.input, result, test.expected)
		}
	}
}

func TestEscapeMarkdownV2(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"file_1.txt", "file\\_1\\.txt"},
		{"[progress]", "\\[progress\\]"},
		{"already\\_escaped", "already\\_escaped"},
		{"lone\\backslash", "lone\\\\backslash"},
		{"mixed *bold* and _italic_", "mixed \\*bold\\* and \\_italic\\_"},
		{"| pipes |", "\\| pipes \\|"},
	}

	for _, test := range tests {
		result := EscapeMarkdownV2(test.input)
		if result != test.expected {
			t.Errorf("EscapeMarkdownV2(%s) = %s; want %s", test.input, result, test.expected)
		}
	}
}

func TestParseBytesString(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"1.5GB", 1500000000},
		{"500MB", 500000000},
		{"100KB", 100000},
		{"2048B", 2048},
		{"1.25GiB", 1342177280},
		{"256MiB", 268435456},
		{"512KiB", 524288},
		{"invalid", 0},
		{"", 0},
		{"10.5KB", 10500},
		{"0.5MB", 500000},
	}

	for _, test := range tests {
		result := ParseBytesString(test.input)
		if result != test.expected {
			t.Errorf("ParseBytesString(%s) = %d; want %d", test.input, result, test.expected)
		}
	}
}

func TestExtractURLFromText(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Check out https://example.com for more", "https://example.com"},
		{"Visit http://test.org/path", "http://test.org/path"},
		{"No URL here", ""},
		{"Multiple https://first.com and https://second.com URLs", "https://first.com"},
		{"", ""},
	}

	for _, test := range tests {
		result := ExtractURLFromText(test.input)
		if result != test.expected {
			t.Errorf("ExtractURLFromText(%q) = %q; want %q", test.input, result, test.expected)
		}
	}
}

func TestExtractMagnetFromText(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Download: magnet:?xt=urn:btih:abc123", "magnet:?xt=urn:btih:abc123"},
		{"No magnet here", ""},
		{"", ""},
		{"Text before magnet:?xt=urn:btih:xyz789 text after", "magnet:?xt=urn:btih:xyz789"},
	}

	for _, test := range tests {
		result := ExtractMagnetFromText(test.input)
		if result != test.expected {
			t.Errorf("ExtractMagnetFromText(%q) = %q; want %q", test.input, result, test.expected)
		}
	}
}

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		url      string
		password string
		quality  string
		fileName string
		zip      bool
		unzip    bool
	}{
		{
			name:     "URL with zip flag",
			input:    "https://example.com/file.zip -z",
			url:      "https://example.com/file.zip",
			zip:      true,
			unzip:    false,
			password: "",
			quality:  "",
			fileName: "",
		},
		{
			name:     "URL with unzip flag",
			input:    "https://example.com/file.zip -uz",
			url:      "https://example.com/file.zip",
			zip:      false,
			unzip:    true,
			password: "",
			quality:  "",
			fileName: "",
		},
		{
			name:     "URL with password",
			input:    "https://example.com/file.zip -p secret123",
			url:      "https://example.com/file.zip",
			zip:      false,
			unzip:    false,
			password: "secret123",
			quality:  "",
			fileName: "",
		},
		{
			name:     "URL with quality",
			input:    "https://youtube.com/watch?v=123 -q 1080",
			url:      "https://youtube.com/watch?v=123",
			zip:      false,
			unzip:    false,
			password: "",
			quality:  "1080",
			fileName: "",
		},
		{
			name:     "URL with custom name",
			input:    "https://example.com/file.zip -n MyFile.zip",
			url:      "https://example.com/file.zip",
			zip:      false,
			unzip:    false,
			password: "",
			quality:  "",
			fileName: "MyFile.zip",
		},
		{
			name:     "Multiple flags combined",
			input:    "https://example.com/file.zip -z -p secret -n CustomName.zip",
			url:      "https://example.com/file.zip",
			zip:      true,
			unzip:    false,
			password: "secret",
			quality:  "",
			fileName: "CustomName.zip",
		},
		{
			name:     "No flags",
			input:    "https://example.com/file.zip",
			url:      "https://example.com/file.zip",
			zip:      false,
			unzip:    false,
			password: "",
			quality:  "",
			fileName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, zip, unzip, password, quality, fileName, _, _ := ParseFlags(tt.input)

			if url != tt.url {
				t.Errorf("URL = %q; want %q", url, tt.url)
			}
			if zip != tt.zip {
				t.Errorf("Zip = %v; want %v", zip, tt.zip)
			}
			if unzip != tt.unzip {
				t.Errorf("Unzip = %v; want %v", unzip, tt.unzip)
			}
			if password != tt.password {
				t.Errorf("Password = %q; want %q", password, tt.password)
			}
			if quality != tt.quality {
				t.Errorf("Quality = %q; want %q", quality, tt.quality)
			}
			if fileName != tt.fileName {
				t.Errorf("FileName = %q; want %q", fileName, tt.fileName)
			}
		})
	}
}

func TestScanLinesWithCR(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		token    []byte
		advance  int
		atEOF    bool
		hasError bool
	}{
		{
			name:     "Line ending with LF",
			input:    []byte("hello\nworld"),
			atEOF:    false,
			advance:  6,
			token:    []byte("hello"),
			hasError: false,
		},
		{
			name:     "Line ending with CRLF",
			input:    []byte("hello\r\nworld"),
			atEOF:    false,
			advance:  6,
			token:    []byte("hello"),
			hasError: false,
		},
		{
			name:     "Line ending with CR only",
			input:    []byte("hello\rworld"),
			atEOF:    false,
			advance:  6,
			token:    []byte("hello"),
			hasError: false,
		},
		{
			name:     "At EOF with data",
			input:    []byte("hello"),
			atEOF:    true,
			advance:  5,
			token:    []byte("hello"),
			hasError: false,
		},
		{
			name:     "Empty at EOF",
			input:    []byte(""),
			atEOF:    true,
			advance:  0,
			token:    nil,
			hasError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			advance, token, err := ScanLinesWithCR(tt.input, tt.atEOF)

			if tt.hasError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tt.hasError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if advance != tt.advance {
				t.Errorf("Advance = %d; want %d", advance, tt.advance)
			}
			if string(token) != string(tt.token) {
				t.Errorf("Token = %q; want %q", string(token), string(tt.token))
			}
		})
	}
}

func TestParseFlagsExtended(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		url      string
		password string
		quality  string
		fileName string
		subs     string
		zip      bool
		unzip    bool
		hardsub  bool
	}{
		{
			name:    "hardsub short flag",
			input:   "https://example.com/video.mp4 -hs",
			url:     "https://example.com/video.mp4",
			hardsub: true,
		},
		{
			name:    "hardsub long flag",
			input:   "https://example.com/video.mp4 -hardsub",
			url:     "https://example.com/video.mp4",
			hardsub: true,
		},
		{
			name:  "subtitle language short flag",
			input: "https://example.com/video.mp4 -s en",
			url:   "https://example.com/video.mp4",
			subs:  "en",
		},
		{
			name:  "subtitle languages long flag with list",
			input: "https://example.com/video.mp4 -subs en,es,id",
			url:   "https://example.com/video.mp4",
			subs:  "en,es,id",
		},
		{
			name:  "magnet input with zip flag",
			input: "magnet:?xt=urn:btih:abc123 -z",
			url:   "magnet:?xt=urn:btih:abc123",
			zip:   true,
		},
		{
			name:  "password flag at end without value yields empty password",
			input: "https://example.com/file.zip -p",
			url:   "https://example.com/file.zip",
		},
		{
			name:  "unknown tokens are ignored when no URL present",
			input: "just random text here",
		},
		{
			name:     "multi-word name joined with spaces",
			input:    "https://example.com/file.zip -n My Cool File.zip",
			url:      "https://example.com/file.zip",
			fileName: "My Cool File.zip",
		},
		{
			name:     "name stops at next flag",
			input:    "https://example.com/file.zip -n Foo -z",
			url:      "https://example.com/file.zip",
			fileName: "Foo",
			zip:      true,
		},
		{
			name:     "name stops at magnet link",
			input:    "-n My Torrent magnet:?xt=urn:btih:xyz",
			url:      "magnet:?xt=urn:btih:xyz",
			fileName: "My Torrent",
		},
		{
			name:     "all flags combined",
			input:    "magnet:?xt=urn:btih:mix -z -uz -p secret -q 720 -hs -s en -n Batch One",
			url:      "magnet:?xt=urn:btih:mix",
			password: "secret",
			quality:  "720",
			fileName: "Batch One",
			subs:     "en",
			zip:      true,
			unzip:    true,
			hardsub:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, zip, unzip, password, quality, fileName, subs, hardsub := ParseFlags(tt.input)

			if url != tt.url {
				t.Errorf("URL = %q; want %q", url, tt.url)
			}
			if zip != tt.zip {
				t.Errorf("Zip = %v; want %v", zip, tt.zip)
			}
			if unzip != tt.unzip {
				t.Errorf("Unzip = %v; want %v", unzip, tt.unzip)
			}
			if password != tt.password {
				t.Errorf("Password = %q; want %q", password, tt.password)
			}
			if quality != tt.quality {
				t.Errorf("Quality = %q; want %q", quality, tt.quality)
			}
			if fileName != tt.fileName {
				t.Errorf("FileName = %q; want %q", fileName, tt.fileName)
			}
			if subs != tt.subs {
				t.Errorf("Subs = %q; want %q", subs, tt.subs)
			}
			if hardsub != tt.hardsub {
				t.Errorf("Hardsub = %v; want %v", hardsub, tt.hardsub)
			}
		})
	}
}

func TestExtractDest(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"plain destination", "gdrive:/Backup", "gdrive:/Backup"},
		{"destination with zip flag", "gdrive:/Backup -z", "gdrive:/Backup"},
		{"destination with value flags", "gdrive:/Backup -p secret -q 1080", "gdrive:/Backup"},
		{"URL only yields empty", "https://example.com/file.zip -z", ""},
		{"flags only yield empty", "-z -uz -hs", ""},
		{"empty input", "", ""},
		{"multi-word name consumed before dest", "gdrive:/Backup -n My Name -z", "gdrive:/Backup"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractDest(tt.input)
			if result != tt.expected {
				t.Errorf("ExtractDest(%q) = %q; want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGetMagnetFileName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"magnet:?xt=urn:btih:abc&dn=My+File.zip", "My File.zip"},
		{"magnet:?dn=movie.mkv&tr=http://tracker.example", "movie.mkv"},
		{"magnet:?xt=urn:btih:abc", "unknown_magnet"},
	}

	for _, tt := range tests {
		if got := getMagnetFileName(tt.input); got != tt.expected {
			t.Errorf("getMagnetFileName(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}

func TestGetPathFileName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/downloads/movie.mkv", "movie.mkv"},
		{"/stream/playlist.m3u8", "stream"},
		{"/playlist.m3u8", ""},
		{"playlist.m3u8", "playlist.m3u8"},
		{"/", ""},
		{"", ""},
	}

	for _, tt := range tests {
		if got := getPathFileName(tt.input); got != tt.expected {
			t.Errorf("getPathFileName(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}

func TestGetFallbackFileName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://cdn.host.com/files/report.pdf?token=x", "report.pdf"},
		{"weird-string", "weird-string"},
		{"https://host.com/", "host.com"},
		{"https://host.com/no-ext", "no-ext"},
	}

	for _, tt := range tests {
		if got := getFallbackFileName(tt.input); got != tt.expected {
			t.Errorf("getFallbackFileName(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}

func TestResolveFileName(t *testing.T) {
	t.Run("content disposition wins", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Disposition", `attachment; filename="Report Q3.pdf"`)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		if got := ResolveFileName(srv.URL + "/anything"); got != "Report Q3.pdf" {
			t.Errorf("ResolveFileName = %q; want %q", got, "Report Q3.pdf")
		}
	})

	t.Run("redirect falls back to final url name", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/final/video.mkv", http.StatusFound)
		})
		mux.HandleFunc("/final/video.mkv", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()

		if got := ResolveFileName(srv.URL + "/start"); got != "video.mkv" {
			t.Errorf("ResolveFileName = %q; want %q", got, "video.mkv")
		}
	})

	t.Run("unreachable yields empty", func(t *testing.T) {
		if got := ResolveFileName("http://127.0.0.1:1/x"); got != "" {
			t.Errorf("ResolveFileName = %q; want empty", got)
		}
	})

	t.Run("invalid url yields empty", func(t *testing.T) {
		if got := ResolveFileName("::bad::"); got != "" {
			t.Errorf("ResolveFileName = %q; want empty", got)
		}
	})
}

func TestIsGenericName(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"550e8400-e29b-41d4-a716-446655440000", true},
		{"deadbeefdeadbeef", true},
		{"abc123", false},
		{"movie.mkv", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := IsGenericName(tt.input); got != tt.expected {
			t.Errorf("IsGenericName(%q) = %v; want %v", tt.input, got, tt.expected)
		}
	}
}

func TestIsValidURLAndMagnet(t *testing.T) {
	if !IsValidURL("https://example.com") || !IsValidURL("magnet:?xt=1") || IsValidURL("ftp2://x") || IsValidURL("") {
		t.Error("IsValidURL classification wrong")
	}
	if !IsMagnetLink("magnet:?xt=urn:btih:abc") || IsMagnetLink("https://example.com") {
		t.Error("IsMagnetLink classification wrong")
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		maxLen   int
	}{
		{"short", "short", 10},
		{"exactly10!", "exactly10!", 10},
		{"a-very-long-name", "a-very-l…", 9},
		{"abcdef", "abc", 3},
	}

	for _, tt := range tests {
		if got := TruncateString(tt.input, tt.maxLen); got != tt.expected {
			t.Errorf("TruncateString(%q,%d) = %q; want %q", tt.input, tt.maxLen, got, tt.expected)
		}
	}
}

func TestFormatStatusEmojiBoolToEmoji(t *testing.T) {
	if FormatStatus("") != "" || FormatStatus("queued") != "Queued" {
		t.Error("FormatStatus wrong")
	}
	if got := StatusEmoji("completed"); got != "✅" {
		t.Errorf("StatusEmoji(completed) = %q", got)
	}
	if got := StatusEmoji("nonsense"); got != "❓" {
		t.Errorf("StatusEmoji(nonsense) = %q", got)
	}
	if BoolToEmoji(true) == BoolToEmoji(false) {
		t.Error("BoolToEmoji wrong")
	}
}

func TestParseIntAndGetLastLines(t *testing.T) {
	if ParseInt("42") != 42 || ParseInt("nope") != 0 {
		t.Error("ParseInt wrong")
	}
	logs := "line1\nline2\nline3"
	if GetLastLines(logs, 2) != "line2\nline3" {
		t.Error("GetLastLines tail wrong")
	}
	if GetLastLines(logs, 5) != logs && strings.TrimSpace(GetLastLines(logs, 5)) != logs {
		t.Error("GetLastLines should return input when shorter than n")
	}
}

func TestCopyFileAndCalculateDirSize(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	want := "hello copy"
	if err := os.WriteFile(src, []byte(want), 0o600); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(dir, "sub", "dst.txt")
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := CopyFile(src, dst); err != nil {
		t.Fatalf("CopyFile: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != want {
		t.Errorf("copied content = %q; want %q", got, want)
	}
	if err := CopyFile(filepath.Join(dir, "missing"), dst); err == nil {
		t.Error("CopyFile on missing source should error")
	}

	size, err := CalculateDirSize(dir)
	if err != nil {
		t.Fatalf("CalculateDirSize: %v", err)
	}
	if size != int64(len(want))*2 {
		t.Errorf("CalculateDirSize = %d; want %d", size, int64(len(want))*2)
	}
}

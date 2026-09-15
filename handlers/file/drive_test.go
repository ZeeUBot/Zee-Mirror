package file

import (
	"strings"
	"testing"

	"zee-mirror/internal/service"
)

func TestGetFileIcon(t *testing.T) {
	if got := getFileIcon("movie.MP4"); got == "" {
		t.Fatal("expected non-empty icon")
	}
	if getFileIcon("a.mkv") != getFileIcon("b.mp4") {
		t.Error("same category extensions should share icon")
	}
	if getFileIcon("a.mp4") == getFileIcon("b.mp3") {
		t.Error("video and audio icons should differ")
	}
	if got := getFileIcon("notes.txt"); got != IconFile {
		t.Errorf("unknown extension should fall back to IconFile, got %q", got)
	}
}

func TestFormatDriveFileList(t *testing.T) {
	empty := formatDriveFileList("/root", nil)
	if !strings.Contains(empty, "kosong") {
		t.Error("empty folder should report kosong")
	}

	files := []service.DriveFile{
		{Name: "sub", IsDir: true},
		{Name: "movie.mkv", Size: 1024},
	}
	out := formatDriveFileList("/root", files)
	for _, want := range []string{"FOLDERS", "FILES", "sub", "movie.mkv", "Folders: `1`", "Files: `1`"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

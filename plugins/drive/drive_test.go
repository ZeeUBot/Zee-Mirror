package drive

import (
	"testing"
)

func TestConstructScrapeURL(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		originalURL string
		want        string
		isFolder    bool
	}{
		{
			name:        "Empty ID",
			id:          "",
			originalURL: "https://example.com",
			want:        "https://example.com",
			isFolder:    false,
		},
		{
			name:        "File ID",
			id:          "12345",
			originalURL: "https://drive.usercontent.google.com/...",
			want:        "https://drive.google.com/file/d/12345/view",
			isFolder:    false,
		},
		{
			name:        "Folder ID",
			id:          "folder123",
			originalURL: "https://drive.google.com/drive/folders/folder123",
			want:        "https://drive.google.com/drive/folders/folder123",
			isFolder:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConstructScrapeURL(tt.id, tt.isFolder, tt.originalURL)
			if got != tt.want {
				t.Errorf("ConstructScrapeURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractDriveID(t *testing.T) {
	tests := []struct {
		urlStr       string
		wantID       string
		wantIsFolder bool
	}{
		{"https://drive.google.com/drive/folders/1abcDEfg", "1abcDEfg", true},
		{"https://drive.google.com/file/d/1xyzABC/view", "1xyzABC", false},
		{"https://drive.usercontent.google.com/download?id=1xyzABC&export=download", "1xyzABC", false},
		{"https://docs.google.com/uc?id=1xyzABC", "1xyzABC", false},
		{"invalid_url", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.urlStr, func(t *testing.T) {
			gotID, gotIsFolder := ExtractDriveID(tt.urlStr)
			if gotID != tt.wantID {
				t.Errorf("ExtractDriveID() gotID = %v, want %v", gotID, tt.wantID)
			}
			if gotIsFolder != tt.wantIsFolder {
				t.Errorf("ExtractDriveID() gotIsFolder = %v, want %v", gotIsFolder, tt.wantIsFolder)
			}
		})
	}
}

func TestCanHandle(t *testing.T) {
	e := NewEngine(nil)
	cases := []struct {
		url  string
		want bool
	}{
		{"https://drive.google.com/file/d/1xyzABC/view", true},
		{"https://drive.google.com/drive/folders/1abcDEfg", true},
		{"https://docs.google.com/uc?id=1xyzABC", true},
		{"https://drive.google.com/drive/home", false},
		{"https://mega.nz/file/abc", false},
		{"https://example.com/file.zip", false},
	}
	for _, c := range cases {
		if got := e.CanHandle(c.url); got != c.want {
			t.Errorf("CanHandle(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

func TestParseRcloneLine(t *testing.T) {
	p := ParseRcloneLine("Transferred: 12.5 MiB / 100 MiB, 12%, 1.2 MiB/s, ETA 75s")
	if !p.HasSize || p.Total == 0 {
		t.Error("expected size fields parsed")
	}
	if !p.HasProgress || p.Progress != 12 {
		t.Errorf("expected progress 12, got %v", p.Progress)
	}
	if !p.HasSpeed || p.Speed == 0 {
		t.Error("expected speed parsed")
	}
	if !p.HasETA {
		t.Error("expected ETA parsed")
	}

	empty := ParseRcloneLine("Checking...")
	if empty.HasSize || empty.HasProgress || empty.HasSpeed || empty.HasETA {
		t.Error("non-matching line must set no flags")
	}
}

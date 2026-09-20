package downloader

import "testing"

func TestIsNonRangeURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{
			name: "google video-downloads",
			url:  "https://video-downloads.googleusercontent.com/ADGPM2nVw0X4Ggq",
			want: true,
		},
		{
			name: "drive google uc query with ?",
			url:  "https://drive.google.com/uc?export=download&id=abc123",
			want: true,
		},
		{
			name: "drive google uc query with &",
			url:  "https://drive.google.com/uc&id=abc123",
			want: true,
		},
		{
			name: "drive usercontent redirect target",
			url:  "https://drive.usercontent.google.com/download?id=abc123",
			want: true,
		},
		{
			name: "regular https URL",
			url:  "https://example.com/files/movie.mkv",
			want: false,
		},
		{
			name: "hetzner speedtest URL",
			url:  "https://fsn1-speed.hetzner.com/100MB.bin",
			want: false,
		},
		{
			name: "local file URL",
			url:  "file:///var/lib/telegram-bot-api/foo.mkv",
			want: false,
		},
		{
			name: "magnet link",
			url:  "magnet:?xt=urn:btih:abcdef",
			want: false,
		},
		{
			name: "empty string",
			url:  "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNonRangeURL(tt.url); got != tt.want {
				t.Errorf("IsNonRangeURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

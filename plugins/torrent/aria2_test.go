package torrent

import (
	"testing"

	"zee-mirror/internal/domain"
)

func TestBuildAria2OptionsNonRangeURL(t *testing.T) {
	engine := &Aria2Engine{}

	task := &domain.Task{
		URL:       "https://video-downloads.googleusercontent.com/ADGPM2nVw0X4Ggq",
		TotalSize: 2 * 1024 * 1024 * 1024, // 2 GiB
		FileName:  "movie.mkv",
	}

	opts := engine.buildAria2Options(task, "/tmp/out")

	if got := opts["max-connection-per-server"]; got != "1" {
		t.Errorf("max-connection-per-server = %v, want 1", got)
	}
	if got := opts["split"]; got != "1" {
		t.Errorf("split = %v, want 1", got)
	}
	for _, key := range []string{"always-resume", "continue", "check-integrity"} {
		if got := opts[key]; got != "false" {
			t.Errorf("%s = %v, want false (plain GET for non-range server)", key, got)
		}
	}
}

func TestBuildAria2OptionsNormalURLKeepsResume(t *testing.T) {
	engine := &Aria2Engine{}

	task := &domain.Task{
		URL:       "https://fsn1-speed.hetzner.com/100MB.bin",
		TotalSize: 100 * 1024 * 1024,
		FileName:  "100MB.bin",
	}

	opts := engine.buildAria2Options(task, "/tmp/out")

	if got := opts["max-connection-per-server"]; got != "8" {
		t.Errorf("max-connection-per-server = %v, want 8 (100MB -> 8 conns)", got)
	}
	for _, key := range []string{"always-resume", "continue", "check-integrity"} {
		if got := opts[key]; got != "true" {
			t.Errorf("%s = %v, want true for range-capable server", key, got)
		}
	}
}

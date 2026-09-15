package uploader

import (
	"path/filepath"
	"strings"

	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"

	"zee-mirror/internal/config"
	"zee-mirror/internal/domain"
)

// TestMain doubles as the fake "rclone" binary: when ZEE_FAKE_TOOL=1 is set,
// the test binary replays canned stdout/stderr and exits with FAKE_EXIT.
func TestMain(m *testing.M) {
	if os.Getenv("ZEE_FAKE_TOOL") == "1" {
		if out := os.Getenv("FAKE_STDOUT"); out != "" {
			fmt.Fprint(os.Stdout, out)
		}
		if errOut := os.Getenv("FAKE_STDERR"); errOut != "" {
			fmt.Fprint(os.Stderr, errOut)
		}
		code, _ := strconv.Atoi(os.Getenv("FAKE_EXIT"))
		os.Exit(code)
	}
	os.Exit(m.Run())
}

func fakeTool(exit int, stdout, stderr string) func(context.Context, string, ...string) *exec.Cmd {
	return func(_ context.Context, _ string, _ ...string) *exec.Cmd {
		// #nosec G204 G702 -- fixed test-binary invocation, no tainted args
		cmd := exec.Command(os.Args[0], "-fake.tool")
		cmd.Env = append(os.Environ(),
			"ZEE_FAKE_TOOL=1",
			fmt.Sprintf("FAKE_EXIT=%d", exit),
			"FAKE_STDOUT="+stdout,
			"FAKE_STDERR="+stderr,
		)
		return cmd
	}
}

func testUploader(t *testing.T) (*RcloneUploader, *domain.Task) {
	t.Helper()
	cfg := &config.Config{
		ConfigDir:      t.TempDir(),
		RcloneDest:     "gdrive:/MirrorBot",
		RcloneLogLevel: "info",
	}
	local := t.TempDir() + "/movie.mkv"
	if err := os.WriteFile(local, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return NewRcloneUploader(cfg), &domain.Task{
		ID:        "t1",
		FileName:  "movie.mkv",
		LocalPath: local,
	}
}

const statsLine = "Transferred: 750 MiB / 1 GiB, 73%, 50 MiB/s, ETA 5s"

func TestUpload_SuccessWithFakeRclone(t *testing.T) {
	old := execCommand
	t.Cleanup(func() { execCommand = old })
	execCommand = fakeTool(0, "http://fake.link\n", statsLine)

	r, task := testUploader(t)

	var mu sync.Mutex
	var seen []ProgressUpdate

	err := r.Upload(context.Background(), task, func(p ProgressUpdate) {
		mu.Lock()
		seen = append(seen, p)
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if task.RemoteURL != "http://fake.link" {
		t.Errorf("RemoteURL = %q, want direct link", task.RemoteURL)
	}
	if task.RemotePath == "" {
		t.Error("RemotePath not set")
	}

	// rclone.go:720 throttles parsed stats to one update per 2s; fake exits
	// instantly, so only the terminal 100% update must arrive.
	final := false
	mu.Lock()
	for _, p := range seen {
		if p.Progress == 100 && p.TotalSize > 0 {
			final = true
		}
	}
	mu.Unlock()
	if !final {
		t.Fatal("no final 100% progress update")
	}
}

func TestUpload_FakeRcloneFailure(t *testing.T) {
	old := execCommand
	t.Cleanup(func() { execCommand = old })
	execCommand = fakeTool(1, "", "quota exceeded")

	r, task := testUploader(t)
	err := r.Upload(context.Background(), task, func(ProgressUpdate) {})
	if err == nil {
		t.Fatal("expected error when fake rclone exits 1")
	}
	var ext *domain.ExternalError
	if !errors.As(err, &ext) {
		t.Fatalf("error type %T, want *domain.ExternalError", err)
	}
}

// failoverTool fails for the primary remote and succeeds for any dest
// containing "backup", proving the loop moves to the fallback remote.
func failoverTool() func(context.Context, string, ...string) *exec.Cmd {
	return func(_ context.Context, _ string, args ...string) *exec.Cmd {
		dest := ""
		if len(args) > 2 {
			dest = args[2]
		}
		exit := 1
		stdout := "quota exceeded"
		if strings.Contains(dest, "backup") {
			exit = 0
			stdout = "http://fake.link\n" + statsLine
		}
		cmd := exec.Command(os.Args[0], "-test.run=XXXNoSuchTest") // #nosec G204 G702 // fixed test-binary args
		cmd.Env = append(os.Environ(), "ZEE_FAKE_TOOL=1",
			fmt.Sprintf("FAKE_EXIT=%d", exit), "FAKE_STDOUT="+stdout)
		return cmd
	}
}

func TestUpload_FailoverToFallbackRemote(t *testing.T) {
	old := execCommand
	t.Cleanup(func() { execCommand = old })
	execCommand = failoverTool()

	r, task := testUploader(t)
	r.cfg.RcloneDestFallbacks = []string{"backup:/MirrorBot"}

	var mu sync.Mutex
	var finalProgress float64
	err := r.Upload(context.Background(), task, func(p ProgressUpdate) {
		mu.Lock()
		finalProgress = p.Progress
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("failover Upload: %v", err)
	}
	if !strings.Contains(task.RemotePath, "backup") {
		t.Errorf("RemotePath = %q, want fallback remote", task.RemotePath)
	}
	mu.Lock()
	if finalProgress != 100 {
		t.Errorf("final progress = %v, want 100", finalProgress)
	}
	mu.Unlock()
}

func TestFallbackDests(t *testing.T) {
	r := &RcloneUploader{cfg: &config.Config{
		RcloneDestFallbacks:   []string{" backup:/B ", "", "second:/C"},
		SmartAutoOrganization: true,
	}}
	got := r.fallbackDests("Video")
	want := []string{filepath.Join("backup:/B", "Video"), filepath.Join("second:/C", "Video")}
	if len(got) != len(want) {
		t.Fatalf("fallbackDests() = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("dest[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

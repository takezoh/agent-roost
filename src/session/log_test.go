package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogDirPath(t *testing.T) {
	got := LogDirPath("/data")
	if got != "/data/logs" {
		t.Fatalf("got %s, want /data/logs", got)
	}
}

func TestEnsureLogDir(t *testing.T) {
	dir := t.TempDir()
	got := EnsureLogDir(dir)
	if !strings.HasSuffix(got, "/logs") {
		t.Fatalf("expected suffix /logs, got %s", got)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("directory not created: %v", err)
	}
}

func TestLogPath(t *testing.T) {
	dir := t.TempDir()
	got := LogPath(dir, "abc123")
	want := filepath.Join(dir, "logs", "abc123.log")
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}


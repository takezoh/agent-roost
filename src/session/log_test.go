package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogDir(t *testing.T) {
	dir := LogDir(t.TempDir())
	if filepath.Base(dir) != "logs" {
		t.Fatalf("expected logs dir, got %s", dir)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("directory not created: %v", err)
	}
}

func TestLogPath(t *testing.T) {
	tmp := t.TempDir()
	got := LogPath(tmp, "abc123")
	want := filepath.Join(tmp, "logs", "abc123.log")
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestTailCommand(t *testing.T) {
	tmp := t.TempDir()
	got := TailCommand(tmp, "abc123")
	want := "tail -f " + filepath.Join(tmp, "logs", "abc123.log")
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

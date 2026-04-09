package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempFile(t *testing.T, content string) *os.File {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "log.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestSeekToLastNLines_EmptyFile(t *testing.T) {
	f := writeTempFile(t, "")
	off, err := seekToLastNLines(f, 100)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if off != 0 {
		t.Errorf("offset = %d, want 0", off)
	}
}

func TestSeekToLastNLines_FewerLinesThanRequested(t *testing.T) {
	body := "alpha\nbeta\ngamma\n"
	f := writeTempFile(t, body)
	off, err := seekToLastNLines(f, 100)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if off != 0 {
		t.Errorf("offset = %d, want 0 (file has fewer lines than n)", off)
	}
}

func TestSeekToLastNLines_ExactBoundary(t *testing.T) {
	// 5 lines, request the last 3 → offset should land at start of "c".
	lines := []string{"a", "b", "c", "d", "e"}
	body := strings.Join(lines, "\n") + "\n"
	f := writeTempFile(t, body)

	off, err := seekToLastNLines(f, 3)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	tail, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(tail[off:])
	want := "c\nd\ne\n"
	if got != want {
		t.Errorf("offset suffix = %q, want %q", got, want)
	}
}

func TestSeekToLastNLines_NoTrailingNewline(t *testing.T) {
	// Last line has no terminating newline.
	body := "a\nb\nc"
	f := writeTempFile(t, body)
	off, err := seekToLastNLines(f, 2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	tail, _ := os.ReadFile(f.Name())
	got := string(tail[off:])
	if got != "b\nc" {
		t.Errorf("suffix = %q, want b\\nc", got)
	}
}

func TestSeekToLastNLines_ChunkBoundary(t *testing.T) {
	// Build a body larger than tailReadChunk so the scanner has to walk
	// across chunks. Each line: "lineNNNNN\n" (10 bytes) → 8000 lines ≈
	// 80KB > 64KB chunk.
	var b strings.Builder
	for i := 0; i < 8000; i++ {
		b.WriteString("line")
		b.WriteString(fixedWidth5(i))
		b.WriteByte('\n')
	}
	f := writeTempFile(t, b.String())

	off, err := seekToLastNLines(f, 100)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	tail, _ := os.ReadFile(f.Name())
	suffix := string(tail[off:])
	count := strings.Count(suffix, "\n")
	if count != 100 {
		t.Errorf("suffix has %d newlines, want 100", count)
	}
	if !strings.HasPrefix(suffix, "line07900") {
		t.Errorf("suffix start = %q, want line07900", suffix[:9])
	}
}

func fixedWidth5(n int) string {
	s := ""
	for i := 0; i < 5; i++ {
		s = string(rune('0'+(n%10))) + s
		n /= 10
	}
	return s
}

package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"time=2025-01-01T00:00:00Z level=INFO msg=hello", "INFO"},
		{"time=2025-01-01T00:00:00Z level=WARN msg=oops", "WARN"},
		{"time=2025-01-01T00:00:00Z level=ERROR msg=fail", "ERROR"},
		{"time=2025-01-01T00:00:00Z level=DEBUG msg=trace", "DEBUG"},
		{"no level here", ""},
		{"level=INFO", "INFO"},
	}
	for _, tt := range tests {
		got := parseLogLevel(tt.line)
		if got != tt.want {
			t.Errorf("parseLogLevel(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestReadNewLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	os.WriteFile(path, []byte("line1\nline2\n"), 0o644)

	m := NewLogModel(path, nil)
	got, err := m.readNewLines()
	if err != nil {
		t.Fatal(err)
	}
	if got != "line1\nline2" {
		t.Fatalf("got %q, want %q", got, "line1\nline2")
	}
	if m.offset != 12 {
		t.Fatalf("offset = %d, want 12", m.offset)
	}

	// Second read returns empty
	got, err = m.readNewLines()
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}

	// Append and re-read
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("line3\n")
	f.Close()

	got, err = m.readNewLines()
	if err != nil {
		t.Fatal(err)
	}
	if got != "line3" {
		t.Fatalf("got %q, want %q", got, "line3")
	}
}

func TestReadNewLines_Truncated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	os.WriteFile(path, []byte("long content here\n"), 0o644)

	m := NewLogModel(path, nil)
	m.readNewLines()

	// Truncate file
	os.WriteFile(path, []byte("new\n"), 0o644)

	// Should reset offset and return empty (file reopened)
	_, err := m.readNewLines()
	if err != nil {
		t.Fatal(err)
	}
	if m.offset != 0 {
		t.Fatalf("offset should reset to 0, got %d", m.offset)
	}
}

func TestReadNewLines_PartialLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	os.WriteFile(path, []byte("complete\npartial"), 0o644)

	m := NewLogModel(path, nil)
	got, err := m.readNewLines()
	if err != nil {
		t.Fatal(err)
	}
	if got != "complete" {
		t.Fatalf("got %q, want %q", got, "complete")
	}
	if m.buf != "partial" {
		t.Fatalf("buf = %q, want %q", m.buf, "partial")
	}

	// Complete the partial line
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(" done\n")
	f.Close()

	got, err = m.readNewLines()
	if err != nil {
		t.Fatal(err)
	}
	if got != "partial done" {
		t.Fatalf("got %q, want %q", got, "partial done")
	}
}

func TestTrimLines(t *testing.T) {
	lines := "a\nb\nc\nd\ne"
	got := trimLines(lines, 3)
	if got != "c\nd\ne" {
		t.Fatalf("got %q, want %q", got, "c\nd\ne")
	}
}

func TestTrimLines_UnderLimit(t *testing.T) {
	lines := "a\nb"
	got := trimLines(lines, 5)
	if got != lines {
		t.Fatalf("got %q, want %q", got, lines)
	}
}

func TestResetReader(t *testing.T) {
	m := NewLogModel("/old/path", nil)
	m.offset = 100
	m.buf = "leftover"
	m.resetReader("/new/path")
	if m.logPath != "/new/path" {
		t.Fatalf("logPath = %s, want /new/path", m.logPath)
	}
	if m.offset != 0 {
		t.Fatalf("offset = %d, want 0", m.offset)
	}
	if m.buf != "" {
		t.Fatalf("buf = %q, want empty", m.buf)
	}
}

func TestSwitchToTab(t *testing.T) {
	m := NewLogModel("/app.log", nil)
	m.sessionLogPath = "/session.log"

	m.switchToTab(tabSession)
	if m.activeTab != tabSession {
		t.Fatal("expected tabSession")
	}
	if m.logPath != "/session.log" {
		t.Fatalf("logPath = %s, want /session.log", m.logPath)
	}

	m.switchToTab(tabApp)
	if m.logPath != "/app.log" {
		t.Fatalf("logPath = %s, want /app.log", m.logPath)
	}
}

func TestSwitchToTab_NoSessionPath(t *testing.T) {
	m := NewLogModel("/app.log", nil)
	m.switchToTab(tabSession)
	if m.activeTab != tabSession {
		t.Fatal("expected tabSession")
	}
	if m.logPath != "/app.log" {
		t.Fatalf("logPath should stay /app.log when no session path, got %s", m.logPath)
	}
}

package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/take/agent-roost/core"
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

	m := NewLogModel(path, "", nil)
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

	m := NewLogModel(path, "", nil)
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

	m := NewLogModel(path, "", nil)
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
	m := NewLogModel("/old/path", "", nil)
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

func TestSwitchToTab_App(t *testing.T) {
	logDir := t.TempDir()
	m := NewLogModel("/app.log", logDir, nil)
	m.sessions = []sessionTab{
		{sessionID: "abc123456789", label: "abc123", logPath: filepath.Join(logDir, "abc123456789.log")},
	}
	m.switchToTab(1)
	if m.activeTab != 1 {
		t.Fatal("expected tab 1")
	}
	m.switchToTab(tabApp)
	if m.activeTab != tabApp {
		t.Fatal("expected tabApp")
	}
	if m.logPath != "/app.log" {
		t.Fatalf("logPath = %s, want /app.log", m.logPath)
	}
}

func TestSwitchToTab_Session(t *testing.T) {
	logDir := t.TempDir()
	sessLogPath := filepath.Join(logDir, "abc123456789.log")
	m := NewLogModel("/app.log", logDir, nil)
	m.sessions = []sessionTab{
		{sessionID: "abc123456789", label: "abc123", logPath: sessLogPath},
	}

	m.switchToTab(1)
	if m.activeTab != 1 {
		t.Fatal("expected tab index 1")
	}
	if m.logPath != sessLogPath {
		t.Fatalf("logPath = %s, want %s", m.logPath, sessLogPath)
	}
}

func TestRebuildSessionTabs(t *testing.T) {
	logDir := t.TempDir()
	m := NewLogModel("/app.log", logDir, nil)

	sessions := []core.SessionInfo{
		{ID: "abc123456789", Command: "claude"},
		{ID: "def456789012", Command: "gemini"},
	}
	m.rebuildSessionTabs(sessions)

	if len(m.sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(m.sessions))
	}
	if m.sessions[0].label != "abc123" {
		t.Errorf("label = %q, want %q", m.sessions[0].label, "abc123")
	}
	if m.sessions[1].label != "def456" {
		t.Errorf("label = %q, want %q", m.sessions[1].label, "def456")
	}
}

func TestRebuildSessionTabs_NoLogDir(t *testing.T) {
	m := NewLogModel("/app.log", "", nil)
	m.rebuildSessionTabs([]core.SessionInfo{{ID: "abc123456789", Command: "claude"}})
	if len(m.sessions) != 0 {
		t.Fatal("expected no sessions when logDir is empty")
	}
}

func TestRebuildSessionTabs_ActiveTabFallback(t *testing.T) {
	logDir := t.TempDir()
	m := NewLogModel("/app.log", logDir, nil)
	m.sessions = []sessionTab{{sessionID: "abc", label: "abc123", logPath: "/x.log"}}
	m.activeTab = 1

	// Clearing sessions resets active tab to tabApp
	m.rebuildSessionTabs([]core.SessionInfo{})
	if m.activeTab != tabApp {
		t.Fatalf("activeTab = %d, want %d (tabApp)", m.activeTab, tabApp)
	}
}

func TestTabIndexAtX(t *testing.T) {
	logDir := t.TempDir()
	m := NewLogModel("/app.log", logDir, nil)
	m.sessions = []sessionTab{
		{label: "abc123"},
		{label: "def456"},
	}
	// tabLabels = ["[APP]", "abc123", "def456"]
	// [APP] = 5 chars + 1 sep = 0..5
	// abc123 = 6 chars + 1 sep = 6..12
	// def456 = 6 chars = 13..18

	if got := m.tabIndexAtX(0); got != tabApp {
		t.Errorf("X=0: got %d, want tabApp(0)", got)
	}
	if got := m.tabIndexAtX(6); got != 1 {
		t.Errorf("X=6: got %d, want 1", got)
	}
	if got := m.tabIndexAtX(13); got != 2 {
		t.Errorf("X=13: got %d, want 2", got)
	}
}

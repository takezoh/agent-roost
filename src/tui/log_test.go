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
	got, err := readNewLines(m.tabs[0])
	if err != nil {
		t.Fatal(err)
	}
	if got != "line1\nline2" {
		t.Fatalf("got %q, want %q", got, "line1\nline2")
	}
	if m.tabs[0].offset != 12 {
		t.Fatalf("offset = %d, want 12", m.tabs[0].offset)
	}

	// Second read returns empty
	got, err = readNewLines(m.tabs[0])
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

	got, err = readNewLines(m.tabs[0])
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
	readNewLines(m.tabs[0])

	// Truncate file
	os.WriteFile(path, []byte("new\n"), 0o644)

	// Should reset offset and return empty (file reopened)
	_, err := readNewLines(m.tabs[0])
	if err != nil {
		t.Fatal(err)
	}
	if m.tabs[0].offset != 0 {
		t.Fatalf("offset should reset to 0, got %d", m.tabs[0].offset)
	}
}

func TestReadNewLines_PartialLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	os.WriteFile(path, []byte("complete\npartial"), 0o644)

	m := NewLogModel(path, nil)
	got, err := readNewLines(m.tabs[0])
	if err != nil {
		t.Fatal(err)
	}
	if got != "complete" {
		t.Fatalf("got %q, want %q", got, "complete")
	}
	if m.tabs[0].buf != "partial" {
		t.Fatalf("buf = %q, want %q", m.tabs[0].buf, "partial")
	}

	// Complete the partial line
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(" done\n")
	f.Close()

	got, err = readNewLines(m.tabs[0])
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

func TestSwitchToTab_ResetsReader(t *testing.T) {
	m := NewLogModel("/app.log", nil)
	m.tabs = append(m.tabs, &tabState{label: "EVENTS", logPath: "/events.log"})

	m.tabs[0].offset = 100
	m.switchToTab(1)
	m.switchToTab(0)
	// Switching back resets offset to re-read from tail
	if m.tabs[0].offset != 0 {
		t.Fatalf("offset = %d, want 0", m.tabs[0].offset)
	}
	if m.viewport.GetContent() != "" {
		t.Fatalf("viewport should be empty after reset")
	}
}

func TestSwitchToTab_App(t *testing.T) {
	m := NewLogModel("/app.log", nil)
	m.tabs = append(m.tabs, &tabState{label: "EVENTS", logPath: "/events.log"})
	m.switchToTab(1)
	if m.activeTab != 1 {
		t.Fatal("expected tab 1")
	}
	m.switchToTab(0)
	if m.activeTab != 0 {
		t.Fatal("expected 0")
	}
}

func TestSwitchToTab_DynamicTab(t *testing.T) {
	m := NewLogModel("/app.log", nil)
	m.tabs = append(m.tabs,
		&tabState{label: "EVENTS", logPath: "/events.log"},
		&tabState{label: "TRANSCRIPT", logPath: "/transcript.jsonl"},
	)

	m.switchToTab(2)
	if m.activeTab != 2 {
		t.Fatal("expected tab index 2")
	}
}

func TestRebuildTabs(t *testing.T) {
	m := NewLogModel("/app.log", nil)
	m.rebuildTabs("/events.log", "/transcript.jsonl")

	// TRANSCRIPT + EVENTS + LOG = 3
	if len(m.tabs) != 3 {
		t.Fatalf("got %d tabs, want 3", len(m.tabs))
	}
	if m.tabs[0].label != "TRANSCRIPT" {
		t.Errorf("tab[0] = %q, want TRANSCRIPT", m.tabs[0].label)
	}
	if m.tabs[1].label != "EVENTS" {
		t.Errorf("tab[1] = %q, want EVENTS", m.tabs[1].label)
	}
	if m.tabs[2].label != "LOG" {
		t.Errorf("tab[2] = %q, want LOG", m.tabs[2].label)
	}
}

func TestRebuildTabs_NoClaudeActive(t *testing.T) {
	m := NewLogModel("/app.log", nil)
	m.rebuildTabs("", "")
	if len(m.tabs) != 1 { // LOG only
		t.Fatalf("got %d tabs, want 1", len(m.tabs))
	}
}

func TestRebuildTabs_ActiveTabFallback(t *testing.T) {
	m := NewLogModel("/app.log", nil)
	m.tabs = append(m.tabs, &tabState{label: "EVENTS", logPath: "/x.log"})
	m.activeTab = 1

	m.rebuildTabs("", "")
	if m.activeTab != 0 {
		t.Fatalf("activeTab = %d, want %d (0)", m.activeTab, 0)
	}
}

func TestTabIndexAtX(t *testing.T) {
	m := NewLogModel("/app.log", nil)
	m.tabs = append(m.tabs,
		&tabState{label: "EVENTS"},
		&tabState{label: "abc123"},
		&tabState{label: "def456"},
	)
	// tabs = ["LOG", "EVENTS", "abc123", "def456"]
	// LOG = 3 chars + 1 sep → 0..3
	// EVENTS = 6 chars + 1 sep → 4..10
	// abc123 = 6 chars + 1 sep → 11..17
	// def456 = 6 chars → 18..23

	if got := m.tabIndexAtX(0); got != 0 {
		t.Errorf("X=0: got %d, want 0(0)", got)
	}
	if got := m.tabIndexAtX(4); got != 1 {
		t.Errorf("X=4: got %d, want 1", got)
	}
	if got := m.tabIndexAtX(11); got != 2 {
		t.Errorf("X=11: got %d, want 2", got)
	}
}

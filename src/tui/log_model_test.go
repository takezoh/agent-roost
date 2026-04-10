package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/take/agent-roost/proto"
	"github.com/take/agent-roost/state"
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

func sessionWithTranscript(t *testing.T) proto.SessionInfo {
	t.Helper()
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(transcriptPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	eventsPath := filepath.Join(dir, "events.log")
	if err := os.WriteFile(eventsPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write events: %v", err)
	}
	return proto.SessionInfo{
		ID:       "s1",
		WindowID: "w1",
		View: state.View{
			LogTabs: []state.LogTab{
				{Label: "TRANSCRIPT", Path: transcriptPath, Kind: state.TabKindTranscript},
				{Label: "EVENTS", Path: eventsPath, Kind: state.TabKindText},
			},
		},
	}
}

func TestHandleLogEvent_PreviewActivatesInfoTab(t *testing.T) {
	m := NewLogModel("/var/log/roost.log", nil, false)
	sess := sessionWithTranscript(t)

	model, _ := m.handleLogEvent(proto.EvtSessionsChanged{
		Sessions:       []proto.SessionInfo{sess},
		ActiveWindowID: sess.WindowID,
		IsPreview:      true,
	})
	lm := model.(LogModel)
	if !lm.activeTabIs("INFO") {
		t.Errorf("active tab = %q, want INFO", lm.activeTabState().label)
	}
}

func TestHandleLogEvent_PaneFocusedActivatesTranscript(t *testing.T) {
	m := NewLogModel("/var/log/roost.log", nil, false)
	sess := sessionWithTranscript(t)

	// Step 1: preview → INFO becomes active.
	model, _ := m.handleLogEvent(proto.EvtSessionsChanged{
		Sessions:       []proto.SessionInfo{sess},
		ActiveWindowID: sess.WindowID,
		IsPreview:      true,
	})
	lm := model.(LogModel)
	if !lm.activeTabIs("INFO") {
		t.Fatalf("preview did not set INFO active (got %q)", lm.activeTabState().label)
	}

	// Step 2: focus the main pane → should switch to TRANSCRIPT.
	model, _ = lm.handleLogEvent(proto.EvtPaneFocused{
		Pane: mainPane,
	})
	lm = model.(LogModel)
	if !lm.activeTabIs("TRANSCRIPT") {
		t.Errorf("active tab after main-pane focus = %q, want TRANSCRIPT", lm.activeTabState().label)
	}
}

func TestHandleLogEvent_PaneFocusedWithoutTranscriptKeepsInfo(t *testing.T) {
	m := NewLogModel("/var/log/roost.log", nil, false)
	// Session with no driver-provided log tabs (only INFO + LOG will be built).
	sess := proto.SessionInfo{ID: "s1", WindowID: "w1"}

	// First preview to land on INFO.
	model, _ := m.handleLogEvent(proto.EvtSessionsChanged{
		Sessions:       []proto.SessionInfo{sess},
		ActiveWindowID: sess.WindowID,
		IsPreview:      true,
	})
	lm := model.(LogModel)
	if !lm.activeTabIs("INFO") {
		t.Fatalf("preview did not set INFO active (got %q)", lm.activeTabState().label)
	}

	// Focus the main pane — no TRANSCRIPT tab exists, so INFO should be retained.
	model, _ = lm.handleLogEvent(proto.EvtPaneFocused{
		Pane: mainPane,
	})
	lm = model.(LogModel)
	if !lm.activeTabIs("INFO") {
		t.Errorf("active tab = %q, want INFO (no TRANSCRIPT to switch to)", lm.activeTabState().label)
	}
}

// Regression: Tick re-broadcasts sessions-changed with IsPreview=false
// every poll interval. That must NOT clobber whichever tab the user is
// currently looking at, otherwise INFO/LOG/etc become unviewable.
func TestHandleLogEvent_NonPreviewSessionsChangedKeepsCurrentTab(t *testing.T) {
	m := NewLogModel("/var/log/roost.log", nil, false)
	sess := sessionWithTranscript(t)

	// Preview → INFO is active.
	model, _ := m.handleLogEvent(proto.EvtSessionsChanged{
		Sessions:       []proto.SessionInfo{sess},
		ActiveWindowID: sess.WindowID,
		IsPreview:      true,
	})
	lm := model.(LogModel)
	if !lm.activeTabIs("INFO") {
		t.Fatalf("preview did not set INFO active (got %q)", lm.activeTabState().label)
	}

	// Simulate a Tick-driven broadcast (IsPreview=false) for the same session.
	model, _ = lm.handleLogEvent(proto.EvtSessionsChanged{
		Sessions:       []proto.SessionInfo{sess},
		ActiveWindowID: sess.WindowID,
		IsPreview:      false,
	})
	lm = model.(LogModel)
	if !lm.activeTabIs("INFO") {
		t.Errorf("active tab after tick = %q, want INFO (tick must not switch tabs)", lm.activeTabState().label)
	}
}

func TestHandleLogEvent_PaneFocusedNonMainPaneIgnored(t *testing.T) {
	m := NewLogModel("/var/log/roost.log", nil, false)
	sess := sessionWithTranscript(t)

	// Preview → INFO active.
	model, _ := m.handleLogEvent(proto.EvtSessionsChanged{
		Sessions:       []proto.SessionInfo{sess},
		ActiveWindowID: sess.WindowID,
		IsPreview:      true,
	})
	lm := model.(LogModel)
	if !lm.activeTabIs("INFO") {
		t.Fatalf("preview did not set INFO active (got %q)", lm.activeTabState().label)
	}

	// Focusing the sidebar (non-main pane) must not switch tabs.
	model, _ = lm.handleLogEvent(proto.EvtPaneFocused{
		Pane: sidebarPane,
	})
	lm = model.(LogModel)
	if !lm.activeTabIs("INFO") {
		t.Errorf("active tab after sidebar focus = %q, want INFO", lm.activeTabState().label)
	}
}

// Regression: when the server socket closes (e.g. coordinator detached),
// the LogModel must terminate so the tmux pane process exits instead of
// lingering as a zombie. main_model and sessions model already do this.
func TestHandleLogDisconnect_ReturnsQuit(t *testing.T) {
	m := NewLogModel("/var/log/roost.log", nil, false)
	_, cmd := m.Update(logDisconnectMsg{})
	if cmd == nil {
		t.Fatal("expected tea.Quit cmd, got nil")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("expected QuitMsg, got %T", cmd())
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

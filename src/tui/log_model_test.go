package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/state"
)

// testKindTranscript is a test-local TabKind matching what the Claude
// driver would emit. TUI tests must not import driver or lib packages.
const testKindTranscript state.TabKind = "transcript"

// stubRenderer is a minimal TabRenderer for tests.
type stubRenderer struct{}

func (stubRenderer) Append([]byte) string { return "" }
func (stubRenderer) Reset()               {}

func TestMain(m *testing.M) {
	state.RegisterTabRenderer[struct{}](testKindTranscript, func(struct{}) state.TabRenderer {
		return stubRenderer{}
	})
	os.Exit(m.Run())
}

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
		ID: "s1",
		View: state.View{
			LogTabs: []state.LogTab{
				{Label: "TRANSCRIPT", Path: transcriptPath, Kind: testKindTranscript},
				{Label: "EVENTS", Path: eventsPath, Kind: state.TabKindText},
			},
		},
	}
}

func TestHandleLogEvent_PreviewActivatesInfoTab(t *testing.T) {
	m := NewLogModel("/var/log/roost.log", nil)
	sess := sessionWithTranscript(t)

	model, _ := m.handleLogEvent(proto.EvtSessionsChanged{
		Sessions:        []proto.SessionInfo{sess},
		ActiveSessionID: sess.ID,
		IsPreview:       true,
	})
	lm := model.(LogModel)
	if !lm.activeTabIs("INFO") {
		t.Errorf("active tab = %q, want INFO", lm.activeTabState().label)
	}
}

func TestHandleLogEvent_PaneFocusedActivatesTranscript(t *testing.T) {
	m := NewLogModel("/var/log/roost.log", nil)
	sess := sessionWithTranscript(t)

	// Step 1: preview → INFO becomes active.
	model, _ := m.handleLogEvent(proto.EvtSessionsChanged{
		Sessions:        []proto.SessionInfo{sess},
		ActiveSessionID: sess.ID,
		IsPreview:       true,
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
	m := NewLogModel("/var/log/roost.log", nil)
	// Session with no driver-provided log tabs (only INFO + LOG will be built).
	sess := proto.SessionInfo{ID: "s1"}

	// First preview to land on INFO.
	model, _ := m.handleLogEvent(proto.EvtSessionsChanged{
		Sessions:        []proto.SessionInfo{sess},
		ActiveSessionID: sess.ID,
		IsPreview:       true,
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
	m := NewLogModel("/var/log/roost.log", nil)
	sess := sessionWithTranscript(t)

	// Preview → INFO is active.
	model, _ := m.handleLogEvent(proto.EvtSessionsChanged{
		Sessions:        []proto.SessionInfo{sess},
		ActiveSessionID: sess.ID,
		IsPreview:       true,
	})
	lm := model.(LogModel)
	if !lm.activeTabIs("INFO") {
		t.Fatalf("preview did not set INFO active (got %q)", lm.activeTabState().label)
	}

	// Simulate a Tick-driven broadcast (IsPreview=false) for the same session.
	model, _ = lm.handleLogEvent(proto.EvtSessionsChanged{
		Sessions:        []proto.SessionInfo{sess},
		ActiveSessionID: sess.ID,
		IsPreview:       false,
	})
	lm = model.(LogModel)
	if !lm.activeTabIs("INFO") {
		t.Errorf("active tab after tick = %q, want INFO (tick must not switch tabs)", lm.activeTabState().label)
	}
}

func TestHandleLogEvent_PaneFocusedNonMainPaneIgnored(t *testing.T) {
	m := NewLogModel("/var/log/roost.log", nil)
	sess := sessionWithTranscript(t)

	// Preview → INFO active.
	model, _ := m.handleLogEvent(proto.EvtSessionsChanged{
		Sessions:        []proto.SessionInfo{sess},
		ActiveSessionID: sess.ID,
		IsPreview:       true,
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
	m := NewLogModel("/var/log/roost.log", nil)
	_, cmd := m.Update(logDisconnectMsg{})
	if cmd == nil {
		t.Fatal("expected tea.Quit cmd, got nil")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("expected QuitMsg, got %T", cmd())
	}
}

func TestSwitchToTab_RebuildRendererOnTabChange(t *testing.T) {
	m := NewLogModel("/var/log/roost.log", nil)
	sess := sessionWithTranscript(t)

	// Set the active session → TRANSCRIPT tab active, renderer is set.
	model, _ := m.handleLogEvent(proto.EvtSessionsChanged{
		Sessions:        []proto.SessionInfo{sess},
		ActiveSessionID: sess.ID,
	})
	lm := model.(LogModel)
	if lm.renderer == nil {
		t.Fatal("renderer should be non-nil for TRANSCRIPT tab")
	}

	// Switch to EVENTS tab → renderer should become nil (TabKindText has no renderer).
	eventsIdx, ok := lm.tabIndexByLabel("EVENTS")
	if !ok {
		t.Fatal("EVENTS tab not found")
	}
	lm.switchToTab(eventsIdx)
	if lm.renderer != nil {
		t.Error("renderer should be nil for EVENTS tab (TabKindText has no registered renderer)")
	}

	// Switch back to TRANSCRIPT → renderer should be restored.
	transcriptIdx, ok := lm.tabIndexByLabel("TRANSCRIPT")
	if !ok {
		t.Fatal("TRANSCRIPT tab not found")
	}
	lm.switchToTab(transcriptIdx)
	if lm.renderer == nil {
		t.Error("renderer should be non-nil after switching back to TRANSCRIPT tab")
	}
}

func TestAppendContent_PlainTextWhenNoRenderer(t *testing.T) {
	m := NewLogModel("/var/log/roost.log", nil)
	sess := sessionWithTranscript(t)

	model, _ := m.handleLogEvent(proto.EvtSessionsChanged{
		Sessions:        []proto.SessionInfo{sess},
		ActiveSessionID: sess.ID,
	})
	lm := model.(LogModel)

	// Switch to EVENTS (plain text tab)
	eventsIdx, ok := lm.tabIndexByLabel("EVENTS")
	if !ok {
		t.Fatal("EVENTS tab not found")
	}
	lm.switchToTab(eventsIdx)

	// Append plain text — should pass through unmodified.
	lm.appendContent("2026-04-11T03:48:01Z PreToolUse Read /tmp/foo.go")
	content := lm.viewport.GetContent()
	if content == "" {
		t.Fatal("viewport content is empty after appendContent")
	}
	if !strings.Contains(content, "PreToolUse") {
		t.Errorf("content = %q, expected to contain PreToolUse", content)
	}
}

// Regression: new sessions must stream EvtSessionFileLine immediately,
// without requiring a tab switch first. This broke when buildSessionInfos
// switched from r.state.Active to r.activeSession, which was only set after
// swap-pane success — ActiveSessionID arrived empty, pickActiveSession
// returned nil, and all streaming was silently dropped.
func TestHandleLogEvent_SessionFileLineStreams(t *testing.T) {
	m := NewLogModel("/var/log/roost.log", nil)
	sess := sessionWithTranscript(t)

	// Simulate the non-preview sessions-changed event with ActiveSessionID set
	// (as emitted after the Bug 1 fix — r.activeSession set unconditionally).
	model, _ := m.handleLogEvent(proto.EvtSessionsChanged{
		Sessions:        []proto.SessionInfo{sess},
		ActiveSessionID: sess.ID,
		IsPreview:       false,
	})
	lm := model.(LogModel)
	if lm.currentSession == nil {
		t.Fatal("currentSession is nil — ActiveSessionID was not picked up")
	}

	// Use the EVENTS tab (TabKindText) so appendContent passes the line
	// through as plain text (stubRenderer.Append returns "", making
	// TRANSCRIPT tab unsuitable for viewport-content assertions).
	eventsIdx, ok := lm.tabIndexByLabel("EVENTS")
	if !ok {
		t.Fatal("EVENTS tab not found")
	}
	lm.switchToTab(eventsIdx)

	// Deliver a streaming line via EvtSessionFileLine with kind "text".
	model, _ = lm.handleLogEvent(proto.EvtSessionFileLine{
		SessionID: sess.ID,
		Kind:      string(state.TabKindText),
		Line:      "hello from events\n",
	})
	lm = model.(LogModel)
	content := lm.viewport.GetContent()
	if !strings.Contains(content, "hello from events") {
		t.Errorf("viewport content %q does not contain streamed line", content)
	}
}

func TestHandleLogEvent_SessionFileLineDroppedWhenNoActiveSession(t *testing.T) {
	m := NewLogModel("/var/log/roost.log", nil)

	// No sessions-changed event → currentSession is nil.
	model, _ := m.handleLogEvent(proto.EvtSessionFileLine{
		SessionID: "s1",
		Kind:      string(testKindTranscript),
		Line:      "should be dropped\n",
	})
	lm := model.(LogModel)
	content := lm.viewport.GetContent()
	if strings.Contains(content, "should be dropped") {
		t.Errorf("line should be dropped when currentSession is nil, got %q", content)
	}
}

func TestHandleLogEvent_ClearsTabsWhenActiveSessionDisappears(t *testing.T) {
	m := NewLogModel("/var/log/roost.log", nil)
	sess := sessionWithTranscript(t)

	model, _ := m.handleLogEvent(proto.EvtSessionsChanged{
		Sessions:        []proto.SessionInfo{sess},
		ActiveSessionID: sess.ID,
	})
	lm := model.(LogModel)
	if lm.currentSession == nil {
		t.Fatal("currentSession should be set")
	}

	model, _ = lm.handleLogEvent(proto.EvtSessionsChanged{
		Sessions:        []proto.SessionInfo{},
		ActiveSessionID: "",
	})
	lm = model.(LogModel)
	if lm.currentSession != nil {
		t.Fatal("currentSession should be cleared")
	}
	if len(lm.tabs) != 1 || lm.tabs[0].label != "LOG" {
		t.Fatalf("tabs = %#v, want only LOG", lm.tabs)
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

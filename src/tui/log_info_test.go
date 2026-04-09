package tui

import (
	"strings"
	"testing"

	"github.com/take/agent-roost/core"
	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/state"
)

func TestRebuildTabs_InsertsInfoBeforeLog(t *testing.T) {
	m := NewLogModel("/app.log", nil, false)
	current := &core.SessionInfo{ID: "abc123", WindowID: "@1", Project: "/p"}
	m.rebuildTabs("/events.log", "/transcript.jsonl", current)

	want := []string{"TRANSCRIPT", "EVENTS", "INFO", "LOG"}
	if len(m.tabs) != len(want) {
		t.Fatalf("got %d tabs, want %d", len(m.tabs), len(want))
	}
	for i, label := range want {
		if m.tabs[i].label != label {
			t.Errorf("tab[%d] = %q, want %q", i, m.tabs[i].label, label)
		}
	}
}

func TestRebuildTabs_InfoOnlyWhenSessionExists(t *testing.T) {
	m := NewLogModel("/app.log", nil, false)
	m.rebuildTabs("", "", nil)
	if len(m.tabs) != 1 {
		t.Fatalf("got %d tabs, want 1", len(m.tabs))
	}

	current := &core.SessionInfo{ID: "abc", WindowID: "@1"}
	m.rebuildTabs("", "", current)
	if len(m.tabs) != 2 {
		t.Fatalf("got %d tabs, want 2", len(m.tabs))
	}
	if m.tabs[0].label != "INFO" || m.tabs[1].label != "LOG" {
		t.Errorf("tabs = [%s, %s], want [INFO, LOG]", m.tabs[0].label, m.tabs[1].label)
	}
}

func TestHandleLogEvent_PreviewActivatesInfo(t *testing.T) {
	m := NewLogModel("/app.log", nil, false)

	sess := core.SessionInfo{
		ID: "abc123def456", WindowID: "@7", Project: "/p", Title: "hello",
	}
	msg := core.NewEvent("sessions-changed")
	msg.Sessions = []core.SessionInfo{sess}
	msg.ActiveWindowID = "@7"
	msg.IsPreview = true

	updated, _ := m.handleLogEvent(msg)
	got := updated.(LogModel)

	idx, ok := got.tabIndexByLabel("INFO")
	if !ok {
		t.Fatal("INFO tab missing after preview event")
	}
	if got.activeTab != idx {
		t.Errorf("activeTab = %d, want %d (INFO)", got.activeTab, idx)
	}
	logIdx, _ := got.tabIndexByLabel("LOG")
	if idx >= logIdx {
		t.Errorf("INFO index %d should be before LOG index %d", idx, logIdx)
	}
	if !strings.Contains(got.viewport.GetContent(), "hello") {
		t.Errorf("viewport %q should contain session title", got.viewport.GetContent())
	}
}

func TestHandleLogEvent_NonPreviewKeepsTranscriptDefault(t *testing.T) {
	m := NewLogModel("/app.log", nil, false)

	sess := core.SessionInfo{ID: "abc", WindowID: "@1", Project: "/p"}
	msg := core.NewEvent("sessions-changed")
	msg.Sessions = []core.SessionInfo{sess}
	msg.ActiveWindowID = "@1"
	msg.TranscriptPath = "/tmp/transcript.jsonl"
	msg.EventLogPath = "/tmp/events.log"
	msg.IsPreview = false

	updated, _ := m.handleLogEvent(msg)
	got := updated.(LogModel)

	if got.activeTab != 0 {
		t.Errorf("activeTab = %d, want 0 (TRANSCRIPT)", got.activeTab)
	}
	if got.tabs[0].label != "TRANSCRIPT" {
		t.Errorf("first tab = %q, want TRANSCRIPT", got.tabs[0].label)
	}
	if _, ok := got.tabIndexByLabel("INFO"); !ok {
		t.Error("INFO tab should exist even on non-preview event")
	}
}

func TestHandleLogEvent_NonPreviewRefreshesInfoWhenActive(t *testing.T) {
	m := NewLogModel("/app.log", nil, false)

	first := core.SessionInfo{ID: "a", WindowID: "@1", Title: "first"}
	preview := core.NewEvent("sessions-changed")
	preview.Sessions = []core.SessionInfo{first}
	preview.ActiveWindowID = "@1"
	preview.IsPreview = true

	updated, _ := m.handleLogEvent(preview)
	m = updated.(LogModel)
	if !m.activeTabIs("INFO") {
		t.Fatal("INFO should be active after preview event")
	}

	updatedSess := core.SessionInfo{ID: "a", WindowID: "@1", Title: "second"}
	refresh := core.NewEvent("sessions-changed")
	refresh.Sessions = []core.SessionInfo{updatedSess}
	refresh.ActiveWindowID = "@1"
	refresh.IsPreview = false

	updated, _ = m.handleLogEvent(refresh)
	m = updated.(LogModel)

	if !m.activeTabIs("INFO") {
		t.Fatal("INFO should remain active across non-preview refresh")
	}
	if !strings.Contains(m.viewport.GetContent(), "second") {
		t.Errorf("viewport should reflect updated title 'second', got %q", m.viewport.GetContent())
	}
}

func TestSwitchToTab_InfoRendersContent(t *testing.T) {
	m := NewLogModel("/app.log", nil, false)
	m.currentSession = &core.SessionInfo{ID: "xyz", WindowID: "@2", Title: "tab-test"}
	m.tabs = []*tabState{
		{label: "LOG", logPath: "/app.log"},
		{label: "INFO"},
	}
	m.activeTab = 0

	m.switchToTab(1)

	if !m.activeTabIs("INFO") {
		t.Fatalf("activeTab not INFO after switch")
	}
	if !strings.Contains(m.viewport.GetContent(), "tab-test") {
		t.Errorf("viewport should contain session title, got %q", m.viewport.GetContent())
	}
}

func TestFormatSessionInfo_IncludesKeyFields(t *testing.T) {
	s := &core.SessionInfo{
		ID:         "deadbeef-1234",
		Project:    "/home/u/proj",
		WindowID:   "@9",
		Command:    "claude",
		State:      state.StatusRunning,
		Title:      "do the thing",
		LastPrompt: "fix the bug",
		Subjects:   []string{"alpha", "beta"},
		Indicators: []string{"2 tools"},
		Tags:       []session.Tag{{Text: "main"}},
	}
	out := formatSessionInfo(s)

	for _, want := range []string{
		"deadbeef-1234", "/home/u/proj", "@9", "claude",
		"do the thing", "fix the bug", "alpha", "beta", "2 tools", "main",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestFormatSessionInfo_NilReturnsEmpty(t *testing.T) {
	if got := formatSessionInfo(nil); got != "" {
		t.Errorf("nil session should return empty string, got %q", got)
	}
}

func TestPickActiveSession(t *testing.T) {
	sessions := []core.SessionInfo{
		{ID: "a", WindowID: "@1"},
		{ID: "b", WindowID: "@2"},
	}
	got := pickActiveSession(sessions, "@2")
	if got == nil || got.ID != "b" {
		t.Errorf("got %+v, want session b", got)
	}
	if pickActiveSession(sessions, "") != nil {
		t.Error("empty wid should return nil")
	}
	if pickActiveSession(sessions, "@99") != nil {
		t.Error("missing wid should return nil")
	}
}

package driver

import (
	"path/filepath"
	"testing"
)

func TestClaudeView_GenericFieldsAlwaysIncluded(t *testing.T) {
	d := newClaude(t)
	view := d.View()
	if len(view.Card.Tags) == 0 {
		t.Fatal("expected at least the command tag")
	}
	if view.Card.Tags[0].Text != "claude" {
		t.Errorf("first tag = %q, want claude", view.Card.Tags[0].Text)
	}
}

func TestClaudeView_BranchTagAppearsAfterDetection(t *testing.T) {
	ctx := &fakeSessionContext{active: true, id: "sess-1"}
	d := newClaudeFactory()(Deps{Session: ctx}).(*claudeDriver)
	d.detectBranch = func(string) string { return "main" }

	// Manually inject a working dir + run the branch refresh once.
	d.mu.Lock()
	d.workingDir = "/proj"
	d.mu.Unlock()
	d.refreshBranch(timeZero(), "")

	view := d.View()
	if len(view.Card.Tags) != 2 {
		t.Fatalf("tags = %+v, want 2 (command + branch)", view.Card.Tags)
	}
	if view.Card.Tags[1].Text != "main" {
		t.Errorf("branch tag = %q, want main", view.Card.Tags[1].Text)
	}
}

func TestClaudeView_LogTabsIncludesEvents(t *testing.T) {
	ctx := &fakeSessionContext{id: "sess-42"}
	dir := t.TempDir()
	d := newClaudeFactory()(Deps{Session: ctx, EventLogDir: dir}).(*claudeDriver)

	view := d.View()
	var found bool
	for _, lt := range view.LogTabs {
		if lt.Label == "EVENTS" {
			found = true
			want := filepath.Join(dir, "sess-42.log")
			if lt.Path != want {
				t.Errorf("EVENTS path = %q, want %q", lt.Path, want)
			}
			if lt.Kind != TabKindText {
				t.Errorf("EVENTS kind = %q, want %q", lt.Kind, TabKindText)
			}
		}
	}
	if !found {
		t.Errorf("EVENTS tab missing from %+v", view.LogTabs)
	}
}

func TestClaudeView_LogTabsIncludesTranscriptWhenKnown(t *testing.T) {
	d := newClaude(t)
	d.mu.Lock()
	d.transcriptPath = "/path/to/x.jsonl"
	d.mu.Unlock()

	view := d.View()
	var found bool
	for _, lt := range view.LogTabs {
		if lt.Label == "TRANSCRIPT" {
			found = true
			if lt.Kind != TabKindTranscript {
				t.Errorf("TRANSCRIPT kind = %q, want %q", lt.Kind, TabKindTranscript)
			}
		}
	}
	if !found {
		t.Errorf("TRANSCRIPT tab missing from %+v", view.LogTabs)
	}
}

func TestClaudeView_InfoExtrasPopulatedFromCachedFields(t *testing.T) {
	d := newClaude(t)
	d.mu.Lock()
	d.title = "Refactor Driver"
	d.lastPrompt = "make it pure"
	d.workingDir = "/proj"
	d.transcriptPath = "/tmp/x.jsonl"
	d.branchTag = "feat/refactor"
	d.errorCount = 2
	d.currentTool = "Edit"
	d.mu.Unlock()

	view := d.View()
	got := map[string]string{}
	for _, line := range view.InfoExtras {
		got[line.Label] = line.Value
	}

	// InfoExtras carries only fields not duplicated by Tags / Indicators
	// (which the TUI re-renders as bullet sections in the INFO tab).
	want := map[string]string{
		"Title":       "Refactor Driver",
		"Last Prompt": "make it pure",
		"Working Dir": "/proj",
		"Transcript":  "/tmp/x.jsonl",
	}
	for label, expected := range want {
		if got[label] != expected {
			t.Errorf("InfoExtras[%q] = %q, want %q", label, got[label], expected)
		}
	}

	// Fields that ARE shown via Tags/Indicators must NOT appear in InfoExtras
	// to avoid duplicate rendering.
	mustNotHave := []string{"Branch", "Errors", "Subagents", "Tool"}
	for _, label := range mustNotHave {
		if _, present := got[label]; present {
			t.Errorf("InfoExtras[%q] should be absent (duplicated by Tags/Indicators)", label)
		}
	}
}

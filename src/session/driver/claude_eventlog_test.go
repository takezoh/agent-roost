package driver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeEventLog_AppendCreatesFileLazily(t *testing.T) {
	dir := t.TempDir()
	ctx := &fakeSessionContext{id: "sess-x"}
	d := newClaudeFactory()(Deps{Session: ctx, EventLogDir: dir}).(*claudeDriver)

	path := filepath.Join(dir, "sess-x.log")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file should not exist before first append, err = %v", err)
	}

	d.appendEventLog("first")
	d.appendEventLog("second")
	d.Close()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "first") || !strings.Contains(text, "second") {
		t.Errorf("event log missing entries: %q", text)
	}
	if strings.Count(text, "\n") < 2 {
		t.Errorf("expected at least 2 lines, got %q", text)
	}
}

func TestClaudeEventLog_NoOpWithoutSessionID(t *testing.T) {
	dir := t.TempDir()
	d := newClaudeFactory()(Deps{Session: inactiveSessionContext{}, EventLogDir: dir}).(*claudeDriver)
	// Must not panic and must not create any files.
	d.appendEventLog("ignored")
	d.Close()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no files in %q, got %d", dir, len(entries))
	}
}

func TestClaudeEventLog_HandleEventWritesLine(t *testing.T) {
	dir := t.TempDir()
	ctx := &fakeSessionContext{id: "sess-y"}
	d := newClaudeFactory()(Deps{Session: ctx, EventLogDir: dir}).(*claudeDriver)

	d.HandleEvent(AgentEvent{
		Type:  AgentEventStateChange,
		State: "running",
		Log:   "claude is running",
	})
	d.Close()

	body, _ := os.ReadFile(filepath.Join(dir, "sess-y.log"))
	if !strings.Contains(string(body), "claude is running") {
		t.Errorf("expected log line, got %q", body)
	}
}

func TestClaudeDriver_PersistedStateRoundtripBranch(t *testing.T) {
	d := newClaude(t)
	d.RestorePersistedState(map[string]string{
		claudeKeySessionID:       "abc",
		claudeKeyWorkingDir:      "/proj",
		claudeKeyTranscriptPath:  "/tmp/x.jsonl",
		claudeKeyStatus:          "waiting",
		claudeKeyStatusChangedAt: "2026-04-09T10:00:00Z",
		claudeKeyBranchTag:       "main",
		claudeKeyBranchTarget:    "/proj",
		claudeKeyBranchAt:        "2026-04-09T10:00:30Z",
	})

	out := d.PersistedState()
	if out[claudeKeyBranchTag] != "main" {
		t.Errorf("branch_tag lost: %q", out[claudeKeyBranchTag])
	}
	if out[claudeKeyBranchTarget] != "/proj" {
		t.Errorf("branch_target lost: %q", out[claudeKeyBranchTarget])
	}
	if out[claudeKeyBranchAt] != "2026-04-09T10:00:30Z" {
		t.Errorf("branch_at lost: %q", out[claudeKeyBranchAt])
	}
}

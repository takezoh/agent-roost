package driver

import (
	"testing"
	"testing/fstest"
	"time"
)

func newClaude(t *testing.T) *claudeDriver {
	t.Helper()
	d := newClaudeFactory()(Deps{FS: fstest.MapFS{}}).(*claudeDriver)
	return d
}

func TestClaudeDriver_HookEventStateChange(t *testing.T) {
	d := newClaude(t)
	consumed := d.HandleEvent(AgentEvent{
		Type:  AgentEventStateChange,
		State: "running",
		DriverState: map[string]string{
			"session_id":  "abc-123",
			"working_dir": "/proj",
		},
	})
	if !consumed {
		t.Fatal("state-change event should be consumed")
	}
	if got, _ := d.Status(); got.Status != StatusRunning {
		t.Errorf("status = %v, want running", got.Status)
	}
	persisted := d.PersistedState()
	if persisted["session_id"] != "abc-123" {
		t.Errorf("session_id not absorbed, got %q", persisted["session_id"])
	}
	if persisted["working_dir"] != "/proj" {
		t.Errorf("working_dir not absorbed, got %q", persisted["working_dir"])
	}
}

func TestClaudeDriver_HookEventUnparseableStateRejected(t *testing.T) {
	d := newClaude(t)
	if d.HandleEvent(AgentEvent{Type: AgentEventStateChange, State: "garbage"}) {
		t.Errorf("unparseable state should not be consumed")
	}
}

func TestClaudeDriver_PersistedStateRoundtrip(t *testing.T) {
	d := newClaude(t)
	d.RestorePersistedState(map[string]string{
		"session_id":        "old-id",
		"working_dir":       "/work",
		"transcript_path":   "/tmp/x.jsonl",
		"status":            "waiting",
		"status_changed_at": "2026-04-09T10:00:00Z",
	})
	if got, _ := d.Status(); got.Status != StatusWaiting {
		t.Errorf("after restore, status = %v, want waiting", got.Status)
	}
	out := d.PersistedState()
	if out["session_id"] != "old-id" || out["working_dir"] != "/work" || out["transcript_path"] != "/tmp/x.jsonl" {
		t.Errorf("persisted bag lost identity: %+v", out)
	}
	if out["status"] != "waiting" {
		t.Errorf("persisted status = %q, want waiting", out["status"])
	}
}

func TestClaudeDriver_MarkSpawnedResetsToIdleButPreservesIdentity(t *testing.T) {
	d := newClaude(t)
	d.RestorePersistedState(map[string]string{
		"session_id": "abc",
		"status":     "running",
	})
	d.MarkSpawned()
	if got, _ := d.Status(); got.Status != StatusIdle {
		t.Errorf("MarkSpawned should reset to Idle, got %v", got.Status)
	}
	if d.PersistedState()["session_id"] != "abc" {
		t.Errorf("MarkSpawned should preserve session identity")
	}
}

func TestClaudeDriver_SpawnCommandUsesResume(t *testing.T) {
	d := newClaude(t)
	d.RestorePersistedState(map[string]string{"session_id": "abc-xyz"})
	cmd := d.SpawnCommand("claude")
	if cmd == "claude" {
		t.Errorf("expected resume command, got %q", cmd)
	}
	want := "claude --resume abc-xyz"
	if cmd != want {
		t.Errorf("got %q, want %q", cmd, want)
	}
}

func TestClaudeDriver_TickIsNoop(t *testing.T) {
	d := newClaude(t)
	d.RestorePersistedState(map[string]string{"status": "running"})
	d.Tick(time.Now(), nil)
	if got, _ := d.Status(); got.Status != StatusRunning {
		t.Errorf("Tick changed status from running to %v", got.Status)
	}
}

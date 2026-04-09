package driver

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// activeWindow is a minimal WindowInfo used by claudeDriver tests to
// flip the active flag without spinning up the real adapter.
type activeWindow struct {
	active  bool
	project string
}

func (a *activeWindow) WindowID() string                   { return "" }
func (a *activeWindow) AgentPaneID() string                { return "" }
func (a *activeWindow) Project() string                    { return a.project }
func (a *activeWindow) Active() bool                       { return a.active }
func (a *activeWindow) RecentLines(_ int) (string, error)  { return "", nil }

func newClaude(t *testing.T) *claudeDriver {
	t.Helper()
	return newClaudeImpl(Deps{})
}

func newClaudeWithSessionID(t *testing.T, sessionID string) *claudeDriver {
	t.Helper()
	return newClaudeImpl(Deps{SessionID: sessionID})
}

// claudeTitle returns the driver's currently cached transcript title.
// Tests use this in place of the removed Title() reader so the assertion
// code stays terse without bypassing locking.
func claudeTitle(d *claudeDriver) string {
	return d.View().Card.Title
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

// TestClaudeDriver_StateSequence drives the realistic running → waiting →
// running → waiting cycle a Claude turn produces and verifies that every
// transition lands in the StatusInfo and that ChangedAt advances on each
// step. This is the regression test for the "input prompt but status stuck
// at running" bug — if a future change drops or coalesces transitions this
// test will catch it.
func TestClaudeDriver_StateSequence(t *testing.T) {
	d := newClaude(t)
	steps := []struct {
		state string
		want  Status
	}{
		{"running", StatusRunning},
		{"waiting", StatusWaiting},
		{"running", StatusRunning},
		{"waiting", StatusWaiting},
	}
	var prevAt time.Time
	for i, step := range steps {
		ok := d.HandleEvent(AgentEvent{Type: AgentEventStateChange, State: step.state})
		if !ok {
			t.Fatalf("step %d (%s): event not consumed", i, step.state)
		}
		got, _ := d.Status()
		if got.Status != step.want {
			t.Errorf("step %d: status = %v, want %v", i, got.Status, step.want)
		}
		if !got.ChangedAt.After(prevAt) && i > 0 {
			t.Errorf("step %d: ChangedAt did not advance (prev=%v, now=%v)", i, prevAt, got.ChangedAt)
		}
		prevAt = got.ChangedAt
		// Force a measurable delta between steps so ChangedAt comparisons
		// are reliable on systems with coarse monotonic clocks.
		time.Sleep(time.Millisecond)
	}
}

// TestClaudeDriver_StateSequence_LastWriteWins documents the current
// "last write wins" behavior: if a late PostToolUse arrives after the Stop
// (e.g. because each Claude hook fires its own roost claude event process and
// they race over the unix socket), the driver will revert from waiting back
// to running. This is the suspected root cause of the "input prompt but
// status stuck at running" report. If a future change adds sequence numbers
// or monotonic timestamps to suppress out-of-order writes, this test will
// fail and should be updated.
func TestClaudeDriver_StateSequence_LastWriteWins(t *testing.T) {
	d := newClaude(t)
	d.HandleEvent(AgentEvent{Type: AgentEventStateChange, State: "running"})
	d.HandleEvent(AgentEvent{Type: AgentEventStateChange, State: "waiting"})
	// Simulated late-arriving PostToolUse from earlier in the turn.
	d.HandleEvent(AgentEvent{Type: AgentEventStateChange, State: "running"})
	got, _ := d.Status()
	if got.Status != StatusRunning {
		t.Fatalf("documented behavior: late running should win, got %v", got.Status)
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
	// Tick with a nil window (no active state) should be a no-op.
	d.Tick(time.Now(), nil)
	if got, _ := d.Status(); got.Status != StatusRunning {
		t.Errorf("Tick changed status from running to %v", got.Status)
	}
}

// writeClaudeTranscript creates a small JSONL file with a custom-title
// entry so refreshMeta has something observable to pick up.
func writeClaudeTranscript(t *testing.T, title string) (path, sessionID string) {
	t.Helper()
	dir := t.TempDir()
	sessionID = "sid-test"
	path = filepath.Join(dir, sessionID+".jsonl")
	body := `{"type":"custom-title","customTitle":"` + title + `"}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	return path, sessionID
}

func TestClaudeDriver_TickGatedByActiveContext(t *testing.T) {
	d := newClaude(t)
	path, sid := writeClaudeTranscript(t, "first")
	// Seed identity so resolveTranscriptPath() returns a real path.
	d.RestorePersistedState(map[string]string{
		"session_id":      sid,
		"transcript_path": path,
	})
	// Reset title set during the Restore-time refreshMeta so we can verify
	// subsequent Ticks really did or didn't run.
	d.title = ""
	d.tickCounter = 0

	inactive := &activeWindow{active: false}
	active := &activeWindow{active: true}

	// Inactive: drive Tick claudeMetaRefreshTicks times. Should be a no-op.
	for i := 0; i < claudeMetaRefreshTicks; i++ {
		d.Tick(time.Now(), inactive)
	}
	if got := claudeTitle(d); got != "" {
		t.Errorf("inactive Tick should not refresh title, got %q", got)
	}

	// Active: next 5 ticks should trigger one refreshMeta.
	for i := 0; i < claudeMetaRefreshTicks; i++ {
		d.Tick(time.Now(), active)
	}
	if got := claudeTitle(d); got != "first" {
		t.Errorf("active Tick should refresh title to %q, got %q", "first", got)
	}
}

func TestClaudeDriver_HandleEventRefreshesRegardlessOfActive(t *testing.T) {
	d := newClaude(t)
	path, sid := writeClaudeTranscript(t, "from event")

	consumed := d.HandleEvent(AgentEvent{
		Type:  AgentEventStateChange,
		State: "running",
		DriverState: map[string]string{
			"session_id":      sid,
			"transcript_path": path,
		},
	})
	if !consumed {
		t.Fatal("state-change event should be consumed")
	}
	if got := claudeTitle(d); got != "from event" {
		t.Errorf("HandleEvent should refresh title even when inactive, got %q", got)
	}
}

func TestClaudeDriver_CloseForgetsTrackerState(t *testing.T) {
	d := newClaude(t)
	path, sid := writeClaudeTranscript(t, "x")
	d.RestorePersistedState(map[string]string{
		"session_id":      sid,
		"transcript_path": path,
	})
	if d.tracker.Snapshot(sid).Title != "x" {
		t.Fatal("setup: tracker should have title cached")
	}
	d.Close()
	if got := d.tracker.Snapshot(sid); got.Title != "" {
		t.Errorf("Close should drop tracker state, got %+v", got)
	}
}

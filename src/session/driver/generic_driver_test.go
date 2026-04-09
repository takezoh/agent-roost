package driver

import (
	"errors"
	"testing"
	"time"
)

// stubWindow is a deterministic WindowInfo for driver tests. The captured
// content sequence is consumed one entry per RecentLines call.
type stubWindow struct {
	wid     string
	pane    string
	project string
	frames  []string
	idx     int
	err     error
}

func (s *stubWindow) WindowID() string    { return s.wid }
func (s *stubWindow) AgentPaneID() string { return s.pane }
func (s *stubWindow) Project() string     { return s.project }
func (s *stubWindow) RecentLines(_ int) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	if s.idx >= len(s.frames) {
		return s.frames[len(s.frames)-1], nil
	}
	out := s.frames[s.idx]
	s.idx++
	return out, nil
}

func newGeneric(threshold time.Duration) *genericDriver {
	d := newGenericFactory("bash")(Deps{IdleThreshold: threshold}).(*genericDriver)
	return d
}

// First Tick after restore must NOT mutate status — the persisted status
// from RestorePersistedState has to survive observer creation.
func TestGenericDriver_FirstTickEstablishesBaselineOnly(t *testing.T) {
	d := newGeneric(10 * time.Second)
	d.RestorePersistedState(map[string]string{
		"status":            "running",
		"status_changed_at": "2026-04-09T12:00:00Z",
	})
	if got, _ := d.Status(); got.Status != StatusRunning {
		t.Fatalf("after restore, status = %v, want running", got.Status)
	}
	win := &stubWindow{frames: []string{"prompt> "}}
	d.Tick(time.Now(), win)
	if got, _ := d.Status(); got.Status != StatusRunning {
		t.Errorf("first tick changed status to %v, want running unchanged", got.Status)
	}
}

func TestGenericDriver_HashChangeMarksRunning(t *testing.T) {
	d := newGeneric(10 * time.Second)
	now := time.Now()
	win := &stubWindow{frames: []string{"a", "b"}}
	d.Tick(now, win)
	d.Tick(now.Add(time.Second), win)
	if got, _ := d.Status(); got.Status != StatusRunning {
		t.Errorf("hash change should yield Running, got %v", got.Status)
	}
}

func TestGenericDriver_PromptDetected(t *testing.T) {
	d := newGeneric(10 * time.Second)
	now := time.Now()
	win := &stubWindow{frames: []string{"running", "prompt$ "}}
	d.Tick(now, win)
	d.Tick(now.Add(time.Second), win)
	if got, _ := d.Status(); got.Status != StatusWaiting {
		t.Errorf("prompt indicator should yield Waiting, got %v", got.Status)
	}
}

func TestGenericDriver_IdleThreshold(t *testing.T) {
	d := newGeneric(2 * time.Second)
	d.RestorePersistedState(map[string]string{"status": "running"})
	start := time.Now()
	win := &stubWindow{frames: []string{"same", "same", "same"}}
	d.Tick(start, win)                  // baseline (status untouched)
	d.Tick(start.Add(time.Second), win) // hash unchanged, threshold not crossed
	if got, _ := d.Status(); got.Status == StatusIdle {
		t.Errorf("status should not be idle yet")
	}
	d.Tick(start.Add(5*time.Second), win) // threshold crossed
	if got, _ := d.Status(); got.Status != StatusIdle {
		t.Errorf("after threshold, status = %v, want idle", got.Status)
	}
}

func TestGenericDriver_CaptureErrorIsTransient(t *testing.T) {
	d := newGeneric(10 * time.Second)
	d.MarkSpawned()
	win := &stubWindow{err: errors.New("capture-pane: pane not found")}
	d.Tick(time.Now(), win)
	if got, _ := d.Status(); got.Status != StatusIdle {
		t.Errorf("capture error must NOT change status, got %v", got.Status)
	}
}

func TestGenericDriver_PersistedStateRoundtrip(t *testing.T) {
	d := newGeneric(0)
	d.RestorePersistedState(map[string]string{
		"status":            "waiting",
		"status_changed_at": "2026-04-09T08:00:00Z",
	})
	persisted := d.PersistedState()
	if persisted["status"] != "waiting" {
		t.Errorf("persisted status = %q, want waiting", persisted["status"])
	}
	if persisted["status_changed_at"] != "2026-04-09T08:00:00Z" {
		t.Errorf("persisted changed_at = %q", persisted["status_changed_at"])
	}
}

func TestGenericDriver_MarkSpawnedResets(t *testing.T) {
	d := newGeneric(time.Second)
	d.RestorePersistedState(map[string]string{"status": "running"})
	d.MarkSpawned()
	if got, _ := d.Status(); got.Status != StatusIdle {
		t.Errorf("MarkSpawned should reset to Idle, got %v", got.Status)
	}
	if d.primed {
		t.Errorf("MarkSpawned should clear primed flag")
	}
}

package state

import (
	"testing"
)

func TestReduceDetachUsesResponseSync(t *testing.T) {
	s := New()
	_, effects := reduceDetach(s, 1, "req-1", struct{}{})

	var hasSync, hasDetach, hasPersist bool
	for _, eff := range effects {
		switch e := eff.(type) {
		case EffSendResponseSync:
			hasSync = true
			if e.ConnID != 1 || e.ReqID != "req-1" {
				t.Errorf("EffSendResponseSync conn/req = %d/%q, want 1/req-1", e.ConnID, e.ReqID)
			}
		case EffDetachClient:
			hasDetach = true
		case EffPersistSnapshot:
			hasPersist = true
		case EffSendResponse:
			t.Errorf("reduceDetach should use EffSendResponseSync, got async EffSendResponse")
		}
	}
	if !hasPersist {
		t.Error("expected EffPersistSnapshot in effects")
	}
	if !hasSync {
		t.Error("expected EffSendResponseSync in effects")
	}
	if !hasDetach {
		t.Error("expected EffDetachClient in effects")
	}
}

func TestReduceShutdownEffects(t *testing.T) {
	s := New()
	_, effects := reduceShutdown(s, 1, "req-1", struct{}{})

	var hasSync, hasKill, hasPersist, hasRelease bool
	for _, eff := range effects {
		switch eff.(type) {
		case EffSendResponseSync:
			hasSync = true
		case EffKillSession:
			hasKill = true
		case EffPersistSnapshot:
			hasPersist = true
		case EffReleaseFrameSandboxes:
			hasRelease = true
		}
	}
	if !hasPersist {
		t.Error("expected EffPersistSnapshot in effects")
	}
	if !hasSync {
		t.Error("expected EffSendResponseSync in effects")
	}
	if !hasRelease {
		t.Error("expected EffReleaseFrameSandboxes in effects")
	}
	if !hasKill {
		t.Error("expected EffKillSession in effects")
	}
}

// TestReduceShutdown_emitsSandboxRelease_order verifies that
// EffReleaseFrameSandboxes precedes EffKillSession in the effect list so
// containers receive a clean stop signal before the tmux session is destroyed.
func TestReduceShutdown_emitsSandboxRelease_order(t *testing.T) {
	s := New()
	_, effects := reduceShutdown(s, 1, "req-1", struct{}{})

	releaseIdx, killIdx := -1, -1
	for i, eff := range effects {
		switch eff.(type) {
		case EffReleaseFrameSandboxes:
			releaseIdx = i
		case EffKillSession:
			killIdx = i
		}
	}
	if releaseIdx < 0 {
		t.Fatal("EffReleaseFrameSandboxes not found in shutdown effects")
	}
	if killIdx < 0 {
		t.Fatal("EffKillSession not found in shutdown effects")
	}
	if releaseIdx > killIdx {
		t.Errorf("EffReleaseFrameSandboxes (idx %d) must precede EffKillSession (idx %d)", releaseIdx, killIdx)
	}
}

// TestReduceDetach_omitsSandboxRelease verifies that detach does not release
// sandbox resources — containers must survive for warm-restart adoption.
func TestReduceDetach_omitsSandboxRelease(t *testing.T) {
	s := New()
	_, effects := reduceDetach(s, 1, "req-1", struct{}{})
	for _, eff := range effects {
		if _, ok := eff.(EffReleaseFrameSandboxes); ok {
			t.Error("reduceDetach must not emit EffReleaseFrameSandboxes; containers must survive for warm-restart")
		}
	}
}

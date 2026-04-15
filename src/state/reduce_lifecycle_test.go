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

	var hasSync, hasKill, hasPersist bool
	for _, eff := range effects {
		switch eff.(type) {
		case EffSendResponseSync:
			hasSync = true
		case EffKillSession:
			hasKill = true
		case EffPersistSnapshot:
			hasPersist = true
		}
	}
	if !hasPersist {
		t.Error("expected EffPersistSnapshot in effects")
	}
	if !hasSync {
		t.Error("expected EffSendResponseSync in effects")
	}
	if !hasKill {
		t.Error("expected EffKillSession in effects")
	}
}

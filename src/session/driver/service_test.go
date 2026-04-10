package driver

import (
	"testing"
)

func newTestDriverService() *DriverService {
	return NewDriverService(DefaultRegistry(), Deps{})
}

func TestDriverService_CreateThenGet(t *testing.T) {
	svc := newTestDriverService()
	drv := svc.Create("sid-1", "claude")
	if drv == nil {
		t.Fatal("Create returned nil")
	}
	got, ok := svc.Get("sid-1")
	if !ok || got != drv {
		t.Errorf("Get(sid-1) returned a different instance")
	}
}

func TestDriverService_RestoreFillsState(t *testing.T) {
	svc := newTestDriverService()
	drv := svc.Restore("sid-2", "claude", map[string]string{
		"session_id": "abc",
		"status":     "waiting",
	})
	if got, _ := drv.Status(); got.Status != StatusWaiting {
		t.Errorf("status after restore = %v, want waiting", got.Status)
	}
	if drv.PersistedState()["session_id"] != "abc" {
		t.Error("session_id not propagated through Restore")
	}
}

func TestDriverService_Close(t *testing.T) {
	svc := newTestDriverService()
	svc.Create("sid-3", "bash")
	svc.Close("sid-3")
	if _, ok := svc.Get("sid-3"); ok {
		t.Error("Close did not remove driver")
	}
	// idempotent
	svc.Close("sid-3")
}

func TestDriverService_FallbackForUnknownCommand(t *testing.T) {
	svc := newTestDriverService()
	drv := svc.Create("sid-4", "unknown-tool")
	if drv == nil {
		t.Fatal("fallback driver should never be nil")
	}
	if drv.Name() != "" {
		// fallback genericDriver has empty name (default)
		t.Errorf("fallback driver name = %q, want empty", drv.Name())
	}
}

func TestDriverService_PropagatesSessionID(t *testing.T) {
	svc := newTestDriverService()
	drv := svc.Create("sid-5", "claude")
	cd, ok := unwrapDriver(drv).(*claudeDriver)
	if !ok {
		t.Fatalf("expected wrapped *claudeDriver, got %T", drv)
	}
	if cd.sessionID != "sid-5" {
		t.Errorf("sessionID = %q, want sid-5", cd.sessionID)
	}
}

func TestRegistry_DefaultRegistryRegistersClaude(t *testing.T) {
	r := DefaultRegistry()
	f := r.Resolve("claude")
	d := f(Deps{})
	if d.Name() != "claude" {
		t.Errorf("Resolve(claude) returned driver name = %q", d.Name())
	}
}

func TestKind_StripsEnvAndPath(t *testing.T) {
	cases := map[string]string{
		"claude":                   "claude",
		"claude --worktree":        "claude",
		"FOO=bar /usr/bin/claude":  "claude",
		"":                         "",
	}
	for in, want := range cases {
		if got := Kind(in); got != want {
			t.Errorf("Kind(%q) = %q, want %q", in, got, want)
		}
	}
}

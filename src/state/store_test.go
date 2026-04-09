package state

import (
	"errors"
	"testing"
	"time"
)

type mockOptions struct {
	options    map[string]map[string]string
	failWrite  error
	failUnset  error
	failList   error
}

func newMockOptions() *mockOptions {
	return &mockOptions{options: make(map[string]map[string]string)}
}

func (m *mockOptions) SetWindowUserOptions(windowID string, kv map[string]string) error {
	if m.failWrite != nil {
		return m.failWrite
	}
	if _, ok := m.options[windowID]; !ok {
		m.options[windowID] = make(map[string]string)
	}
	for k, v := range kv {
		m.options[windowID][k] = v
	}
	return nil
}

func (m *mockOptions) UnsetWindowUserOptions(windowID string, keys ...string) error {
	if m.failUnset != nil {
		return m.failUnset
	}
	if _, ok := m.options[windowID]; !ok {
		return nil
	}
	for _, k := range keys {
		delete(m.options[windowID], k)
	}
	return nil
}

func (m *mockOptions) ListWindowOptions() (map[string]map[string]string, error) {
	if m.failList != nil {
		return nil, m.failList
	}
	out := make(map[string]map[string]string, len(m.options))
	for k, v := range m.options {
		opts := make(map[string]string, len(v))
		for kk, vv := range v {
			opts[kk] = vv
		}
		out[k] = opts
	}
	return out, nil
}

func TestStore_SetGet(t *testing.T) {
	mock := newMockOptions()
	s := NewStore(mock)

	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	if err := s.Set("@1", Info{Status: StatusWaiting, ChangedAt: now}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, ok := s.Get("@1")
	if !ok {
		t.Fatal("Get returned ok=false after Set")
	}
	if got.Status != StatusWaiting {
		t.Errorf("Status = %s, want waiting", got.Status)
	}
	if !got.ChangedAt.Equal(now) {
		t.Errorf("ChangedAt = %v, want %v", got.ChangedAt, now)
	}

	if mock.options["@1"][OptionStatus] != "waiting" {
		t.Errorf("tmux %s = %q, want waiting", OptionStatus, mock.options["@1"][OptionStatus])
	}
	if mock.options["@1"][OptionChangedAt] != now.Format(time.RFC3339) {
		t.Errorf("tmux %s = %q, want %s", OptionChangedAt, mock.options["@1"][OptionChangedAt], now.Format(time.RFC3339))
	}
}

func TestStore_SetWriteFailureLeavesCacheUntouched(t *testing.T) {
	mock := newMockOptions()
	s := NewStore(mock)
	if err := s.Set("@1", Info{Status: StatusRunning, ChangedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	mock.failWrite = errors.New("tmux: simulated failure")
	err := s.Set("@1", Info{Status: StatusWaiting, ChangedAt: time.Now()})
	if err == nil {
		t.Fatal("expected Set to return tmux error")
	}

	got, ok := s.Get("@1")
	if !ok {
		t.Fatal("expected Get to still return previous value")
	}
	if got.Status != StatusRunning {
		t.Errorf("Status leaked through failed write: got %s, want running", got.Status)
	}
}

func TestStore_Delete(t *testing.T) {
	mock := newMockOptions()
	s := NewStore(mock)
	now := time.Now()
	s.Set("@1", Info{Status: StatusRunning, ChangedAt: now})

	if err := s.Delete("@1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("@1"); ok {
		t.Error("expected Get to return ok=false after Delete")
	}
	if _, ok := mock.options["@1"][OptionStatus]; ok {
		t.Errorf("tmux %s should be cleared", OptionStatus)
	}
}

func TestStore_LoadFromTmux(t *testing.T) {
	mock := newMockOptions()
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	mock.options["@1"] = map[string]string{
		OptionStatus:    "pending",
		OptionChangedAt: now.Format(time.RFC3339),
	}
	mock.options["@2"] = map[string]string{
		OptionStatus:    "running",
		OptionChangedAt: now.Format(time.RFC3339),
	}
	// @3 has no status — should be skipped without error
	mock.options["@3"] = map[string]string{
		"@some_other_option": "value",
	}
	// @4 has unknown status — should also be skipped
	mock.options["@4"] = map[string]string{
		OptionStatus: "garbage",
	}

	s := NewStore(mock)
	if err := s.LoadFromTmux(mock); err != nil {
		t.Fatal(err)
	}

	if got, ok := s.Get("@1"); !ok || got.Status != StatusPending {
		t.Errorf("@1 = %v ok=%v, want Pending true", got, ok)
	}
	if got, ok := s.Get("@2"); !ok || got.Status != StatusRunning {
		t.Errorf("@2 = %v ok=%v, want Running true", got, ok)
	}
	if _, ok := s.Get("@3"); ok {
		t.Error("@3 should have been skipped (no status option)")
	}
	if _, ok := s.Get("@4"); ok {
		t.Error("@4 should have been skipped (unknown status)")
	}
}

func TestStore_RoundTripAcrossInstances(t *testing.T) {
	mock := newMockOptions()
	storeA := NewStore(mock)
	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	storeA.Set("@1", Info{Status: StatusPending, ChangedAt: now})
	storeA.Set("@2", Info{Status: StatusWaiting, ChangedAt: now})

	// Simulate Coordinator restart: new store reading from the same tmux mock
	storeB := NewStore(mock)
	if err := storeB.LoadFromTmux(mock); err != nil {
		t.Fatal(err)
	}

	if got, ok := storeB.Get("@1"); !ok || got.Status != StatusPending {
		t.Errorf("storeB @1 = %v ok=%v, want Pending true", got, ok)
	}
	if got, ok := storeB.Get("@2"); !ok || got.Status != StatusWaiting {
		t.Errorf("storeB @2 = %v ok=%v, want Waiting true", got, ok)
	}
}

func TestStore_Snapshot(t *testing.T) {
	mock := newMockOptions()
	s := NewStore(mock)
	now := time.Now()
	s.Set("@1", Info{Status: StatusRunning, ChangedAt: now})
	s.Set("@2", Info{Status: StatusWaiting, ChangedAt: now})

	snap := s.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot has %d entries, want 2", len(snap))
	}
	if snap["@1"].Status != StatusRunning {
		t.Errorf("snap @1 = %s, want running", snap["@1"].Status)
	}
	// Mutating the snapshot must not affect the store.
	delete(snap, "@1")
	if _, ok := s.Get("@1"); !ok {
		t.Error("Snapshot mutation leaked back into the store")
	}
}

func TestParseStatus(t *testing.T) {
	cases := []struct {
		in   string
		want Status
		ok   bool
	}{
		{"running", StatusRunning, true},
		{"waiting", StatusWaiting, true},
		{"idle", StatusIdle, true},
		{"stopped", StatusStopped, true},
		{"pending", StatusPending, true},
		{"", 0, false},
		{"garbage", 0, false},
	}
	for _, c := range cases {
		got, ok := ParseStatus(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("ParseStatus(%q) = (%v, %v), want (%v, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

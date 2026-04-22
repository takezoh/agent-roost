package state_test

import (
	"testing"
	"time"

	"github.com/takezoh/agent-roost/state"
)

// mockDriver is a minimal Driver implementation used to exercise the
// registry and fallback-factory resolution logic.
type mockDriver struct {
	name        string
	displayName string
}

func (m *mockDriver) Name() string                             { return m.name }
func (m *mockDriver) DisplayName() string                      { return m.displayName }
func (m *mockDriver) NewState(now time.Time) state.DriverState { return state.DriverStateBase{} }
func (m *mockDriver) Step(prev state.DriverState, ctx state.FrameContext, ev state.DriverEvent) (state.DriverState, []state.Effect, state.View) {
	return prev, nil, state.View{}
}
func (m *mockDriver) Status(s state.DriverState) state.Status       { return state.StatusIdle }
func (m *mockDriver) View(s state.DriverState) state.View           { return state.View{} }
func (m *mockDriver) Persist(s state.DriverState) map[string]string { return nil }
func (m *mockDriver) Restore(bag map[string]string, now time.Time) state.DriverState {
	return state.DriverStateBase{}
}
func (m *mockDriver) PrepareLaunch(s state.DriverState, mode state.LaunchMode, project, baseCommand string, options state.LaunchOptions) (state.LaunchPlan, error) {
	return state.LaunchPlan{}, nil
}

func saveRegistry() map[string]state.Driver {
	snap := make(map[string]state.Driver)
	for _, d := range state.GetRegistry() {
		snap[d.Name()] = d
	}
	return snap
}

func restoreRegistry(snap map[string]state.Driver) {
	state.ClearRegistry()
	for _, d := range snap {
		state.Register(d)
	}
}

func TestGetDriverUsesFallbackFactory(t *testing.T) {
	snap := saveRegistry()
	defer restoreRegistry(snap)

	state.ClearRegistry()

	var factoryCalls []string
	state.RegisterFallbackFactory(func(command string) state.Driver {
		factoryCalls = append(factoryCalls, command)
		name := state.FirstToken(command)
		return &mockDriver{name: name, displayName: name}
	})

	cases := []struct {
		command        string
		wantName       string
		wantFactoryArg string
	}{
		{"unknown", "unknown", "unknown"},
		{"tig status", "tig", "tig status"},
	}
	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			factoryCalls = nil
			d := state.GetDriver(tc.command)
			if d == nil {
				t.Fatalf("GetDriver(%q) returned nil", tc.command)
			}
			if d.Name() != tc.wantName {
				t.Errorf("Name = %q, want %q", d.Name(), tc.wantName)
			}
			if len(factoryCalls) != 1 || factoryCalls[0] != tc.wantFactoryArg {
				t.Errorf("factory calls = %v, want [%q]", factoryCalls, tc.wantFactoryArg)
			}
		})
	}
}

func TestGetDriverPrefersRegistryOverFactory(t *testing.T) {
	snap := saveRegistry()
	defer restoreRegistry(snap)

	state.ClearRegistry()
	state.Register(&mockDriver{name: "explicit", displayName: "Explicit"})

	factoryCalled := false
	state.RegisterFallbackFactory(func(command string) state.Driver {
		factoryCalled = true
		return &mockDriver{name: "factory", displayName: "factory"}
	})

	d := state.GetDriver("explicit --flag")
	if factoryCalled {
		t.Error("fallback factory was called even though explicit driver is registered")
	}
	if d.Name() != "explicit" {
		t.Errorf("Name = %q, want explicit", d.Name())
	}
}

func TestGetDriverReturnsEmptyFallbackWhenNoFactory(t *testing.T) {
	snap := saveRegistry()
	defer restoreRegistry(snap)

	state.ClearRegistry()
	fallback := &mockDriver{name: "", displayName: "fallback"}
	state.Register(fallback)

	d := state.GetDriver("anything goes")
	if d != fallback {
		t.Errorf("GetDriver returned %v, want the registered \"\" fallback", d)
	}
}

func TestFirstToken(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"tig", "tig"},
		{"tig status", "tig"},
		{"tig\tstatus", "tig"},
		{"  leading", ""},
	}
	for _, tc := range cases {
		if got := state.FirstToken(tc.in); got != tc.want {
			t.Errorf("FirstToken(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

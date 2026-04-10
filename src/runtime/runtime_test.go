package runtime

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/take/agent-roost/state"
	"github.com/take/agent-roost/driver"
)

func TestMain(m *testing.M) {
	// Register a fallback driver under "" so reducers that expect a
	// driver for unknown commands can resolve one. The runtime tests
	// don't exercise driver-specific behaviour — they just need
	// SOMETHING in the registry.
	state.Register(driver.NewGenericDriver("", 0))
	os.Exit(m.Run())
}

// === Fake backends for runtime tests ===

type fakeTmuxBackend struct {
	mu          sync.Mutex
	spawnCalls  int
	killCalls   int
	swapCalls   int
	respawnCmds []string
	statusLines []string
	envs        map[string]string
	popups      []string
	alive       map[string]bool
	captured    string
	spawnWID    string
	spawnPane   string
	spawnErr    error
}

func newFakeTmux() *fakeTmuxBackend {
	return &fakeTmuxBackend{
		alive:    map[string]bool{},
		envs:     map[string]string{},
		spawnWID: "@1",
		spawnPane: "%1",
	}
}

func (f *fakeTmuxBackend) SpawnWindow(name, command, startDir string, env map[string]string) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spawnCalls++
	if f.spawnErr != nil {
		return "", "", f.spawnErr
	}
	return f.spawnWID, f.spawnPane, nil
}

func (f *fakeTmuxBackend) KillWindow(string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.killCalls++
	return nil
}
func (f *fakeTmuxBackend) RunChain(ops ...[]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.swapCalls++
	return nil
}
func (f *fakeTmuxBackend) SelectPane(string) error    { return nil }
func (f *fakeTmuxBackend) SetStatusLine(line string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statusLines = append(f.statusLines, line)
	return nil
}
func (f *fakeTmuxBackend) SetEnv(k, v string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.envs[k] = v
	return nil
}
func (f *fakeTmuxBackend) UnsetEnv(k string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.envs, k)
	return nil
}
func (f *fakeTmuxBackend) PaneAlive(target string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.alive[target]
	if !ok {
		return true, nil
	}
	return v, nil
}
func (f *fakeTmuxBackend) RespawnPane(target, cmd string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.respawnCmds = append(f.respawnCmds, cmd)
	return nil
}
func (f *fakeTmuxBackend) CapturePane(string, int) (string, error) {
	return f.captured, nil
}
func (f *fakeTmuxBackend) DetachClient() error { return nil }
func (f *fakeTmuxBackend) KillSession() error  { return nil }
func (f *fakeTmuxBackend) DisplayPopup(w, h, cmd string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.popups = append(f.popups, cmd)
	return nil
}

type recordingPersist struct {
	mu     sync.Mutex
	saves  int
	last   []SessionSnapshot
}

func (r *recordingPersist) Save(s []SessionSnapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.saves++
	r.last = s
	return nil
}
func (r *recordingPersist) Load() ([]SessionSnapshot, error) { return nil, nil }

type recordingEventLog struct {
	mu    sync.Mutex
	lines []string
}

func (r *recordingEventLog) Append(_ state.SessionID, line string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
	return nil
}
func (r *recordingEventLog) Close(state.SessionID) {}
func (r *recordingEventLog) CloseAll()             {}

// === Tests ===

func TestRuntimeStartsAndShutsDown(t *testing.T) {
	r := New(Config{
		SessionName:  "roost-test",
		RoostExe:     "/usr/local/bin/roost",
		TickInterval: 50 * time.Millisecond,
		Tmux:         newFakeTmux(),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = r.Run(ctx)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-r.Done():
	case <-time.After(time.Second):
		t.Fatal("Run did not exit")
	}
}

func TestRuntimeCreateSessionFlow(t *testing.T) {
	tmux := newFakeTmux()
	persist := &recordingPersist{}
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second, // suppress periodic ticks
		Tmux:         tmux,
		Persist:      persist,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = r.Run(ctx) }()

	r.Enqueue(state.EvCmdCreateSession{
		ConnID: 1, ReqID: "r1", Project: "/tmp/test", Command: "stub-fallback",
	})

	// Wait for the spawn callback to land.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tmux.mu.Lock()
		spawned := tmux.spawnCalls
		tmux.mu.Unlock()
		persist.mu.Lock()
		saved := persist.saves
		persist.mu.Unlock()
		if spawned >= 1 && saved >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-r.Done()

	tmux.mu.Lock()
	defer tmux.mu.Unlock()
	if tmux.spawnCalls != 1 {
		t.Errorf("spawnCalls = %d, want 1", tmux.spawnCalls)
	}
	persist.mu.Lock()
	defer persist.mu.Unlock()
	if persist.saves < 1 {
		t.Errorf("persist saves = %d, want ≥1", persist.saves)
	}
	if len(persist.last) != 1 {
		t.Errorf("last snapshot len = %d, want 1", len(persist.last))
	}
}

func TestRuntimeTickFiresHealthChecks(t *testing.T) {
	tmux := newFakeTmux()
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Millisecond,
		Tmux:         tmux,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = r.Run(ctx) }()

	// Wait for several ticks
	time.Sleep(40 * time.Millisecond)
	cancel()
	<-r.Done()
	// Health checks call PaneAlive on the control panes; with our
	// noop default returning alive=true, no respawns should fire.
	tmux.mu.Lock()
	defer tmux.mu.Unlock()
	if len(tmux.respawnCmds) != 0 {
		t.Errorf("expected 0 respawns when panes are alive, got %d", len(tmux.respawnCmds))
	}
}

func TestRuntimeRespawnsDeadPane(t *testing.T) {
	tmux := newFakeTmux()
	tmux.alive["roost-test:0.1"] = false
	r := New(Config{
		SessionName:  "roost-test",
		RoostExe:     "/usr/bin/roost",
		TickInterval: 10 * time.Millisecond,
		Tmux:         tmux,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = r.Run(ctx) }()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		tmux.mu.Lock()
		n := len(tmux.respawnCmds)
		tmux.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-r.Done()
	tmux.mu.Lock()
	defer tmux.mu.Unlock()
	if len(tmux.respawnCmds) == 0 {
		t.Fatal("expected respawn for dead pane")
	}
	if tmux.respawnCmds[0] != "/usr/bin/roost --tui log" {
		t.Errorf("respawn cmd = %q", tmux.respawnCmds[0])
	}
}

func TestSubstitutePlaceholders(t *testing.T) {
	got := substitutePlaceholdersString("{sessionName}:0.1", "myroost", "/r")
	if got != "myroost:0.1" {
		t.Errorf("got %q", got)
	}
	got2 := substitutePlaceholdersString("{roostExe} --tui log", "x", "/r")
	if got2 != "/r --tui log" {
		t.Errorf("got %q", got2)
	}
}

func TestWindowName(t *testing.T) {
	if got := windowName("/foo/bar", "abc"); got != "bar:abc" {
		t.Errorf("got %q, want bar:abc", got)
	}
	if got := windowName("", "abc"); got != "session:abc" {
		t.Errorf("got %q, want session:abc", got)
	}
}

func TestEventTypeName(t *testing.T) {
	cases := []struct {
		ev   state.Event
		want string
	}{
		{state.EvTick{}, "EvTick"},
		{state.EvCmdCreateSession{}, "EvCmdCreateSession"},
		{state.EvCmdHook{}, "EvCmdHook"},
	}
	for _, c := range cases {
		if got := eventTypeName(c.ev); got != c.want {
			t.Errorf("eventTypeName = %q, want %q", got, c.want)
		}
	}
}

// Sanity: ensure interpret receives every effect type without
// crashing. We push a synthetic effect through the loop's enqueue
// path indirectly via a real reducer event.
func TestRuntimeStopSession(t *testing.T) {
	tmux := newFakeTmux()
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         tmux,
	})
	// Inject a session manually.
	r.state.Sessions["abc"] = state.Session{
		ID: "abc", WindowID: "@5", Command: "stub-x",
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = r.Run(ctx) }()

	r.Enqueue(state.EvCmdStopSession{ConnID: 1, ReqID: "r", SessionID: "abc"})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		tmux.mu.Lock()
		n := tmux.killCalls
		tmux.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-r.Done()
	tmux.mu.Lock()
	defer tmux.mu.Unlock()
	if tmux.killCalls != 1 {
		t.Errorf("killCalls = %d, want 1", tmux.killCalls)
	}
}

func TestRuntimeEnqueueDoesNotBlock(t *testing.T) {
	tmux := newFakeTmux()
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         tmux,
	})
	// Don't start Run — just check Enqueue doesn't deadlock when no
	// reader is active.
	var n atomic.Int32
	for i := 0; i < 100; i++ {
		r.Enqueue(state.EvTick{Now: time.Now()})
		n.Add(1)
	}
	// Channel buffer is 256 so 100 fits without dropping.
	if n.Load() != 100 {
		t.Errorf("enqueued %d, want 100", n.Load())
	}
}

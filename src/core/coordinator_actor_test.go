package core

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
)

// stubPanes is a no-op PaneOperator that lets coordinator tests run
// without an actual tmux server. Methods record the call count so tests
// can assert on dispatch ordering when they care.
type stubPanes struct {
	mu             sync.Mutex
	swapCalls      int
	selectCalls    int
	displayMessage func(target, format string) (string, error)
}

func (p *stubPanes) SwapPane(string, string) error             { return nil }
func (p *stubPanes) RespawnPane(string, string) error          { return nil }
func (p *stubPanes) RunChain(commands ...[]string) error {
	p.mu.Lock()
	p.swapCalls++
	p.mu.Unlock()
	return nil
}
func (p *stubPanes) SelectPane(string) error {
	p.mu.Lock()
	p.selectCalls++
	p.mu.Unlock()
	return nil
}
func (p *stubPanes) DisplayMessage(target, format string) (string, error) {
	if p.displayMessage != nil {
		return p.displayMessage(target, format)
	}
	return "", nil
}

// stubTmuxClient is a no-op session.TmuxClient that records sessions
// added through Create / Spawn so the actor tests can drive Coordinator
// without a live tmux server.
type stubTmuxClient struct {
	mu       sync.Mutex
	created  int
	killed   int
	windows  []session.RoostWindow
	nextWID  int
	nextPane int
}

func (c *stubTmuxClient) NewWindow(name, command, startDir string, env map[string]string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.created++
	c.nextWID++
	wid := "@w" + itoa(c.nextWID)
	// Track the window so ListRoostWindows reports it back during
	// reconcile — otherwise the reaper would evict every freshly
	// created session before the next tick fan-out fires.
	c.windows = append(c.windows, session.RoostWindow{
		ID:       wid,
		WindowID: wid,
		Project:  startDir,
	})
	return wid, nil
}

func (c *stubTmuxClient) KillWindow(windowID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.killed++
	for i, w := range c.windows {
		if w.WindowID == windowID {
			c.windows = append(c.windows[:i], c.windows[i+1:]...)
			break
		}
	}
	return nil
}

func (c *stubTmuxClient) SetOption(target, key, value string) error                  { return nil }
func (c *stubTmuxClient) SetWindowUserOption(windowID, key, value string) error      { return nil }
func (c *stubTmuxClient) SetWindowUserOptions(windowID string, kv map[string]string) error {
	return nil
}

func (c *stubTmuxClient) ListRoostWindows() ([]session.RoostWindow, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]session.RoostWindow, len(c.windows))
	copy(out, c.windows)
	return out, nil
}

func (c *stubTmuxClient) DisplayMessage(target, format string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextPane++
	return "%p" + itoa(c.nextPane), nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// newTestCoordinator builds a Coordinator wired to in-memory stubs.
// The data dir lives under t.TempDir() so the snapshot file is cleaned
// up automatically.
func newTestCoordinator(t *testing.T) *Coordinator {
	t.Helper()
	dataDir := t.TempDir()
	tmuxStub := &stubTmuxClient{}
	sessions := session.NewSessionService(tmuxStub, dataDir)
	drivers := driver.NewDriverService(driver.DefaultRegistry(), driver.Deps{})
	return NewCoordinator(sessions, drivers, &stubPanes{}, nil, "roost", "")
}

func TestCoordinator_StartShutdownIsClean(t *testing.T) {
	c := newTestCoordinator(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx, 50*time.Millisecond)
	c.Shutdown()
	// Idempotent
	c.Shutdown()
}

func TestCoordinator_AllSessionInfosRoutesThroughActor(t *testing.T) {
	c := newTestCoordinator(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx, 50*time.Millisecond)
	defer c.Shutdown()

	id, err := c.Create("/proj", "bash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == "" {
		t.Fatal("Create returned empty id")
	}
	infos := c.AllSessionInfos()
	if len(infos) != 1 {
		t.Fatalf("AllSessionInfos = %d, want 1", len(infos))
	}
	if infos[0].ID != id {
		t.Errorf("info id = %q, want %q", infos[0].ID, id)
	}
	if infos[0].Project != "/proj" {
		t.Errorf("info project = %q, want /proj", infos[0].Project)
	}
}

func TestCoordinator_NotifySessionsChangedFiresOnTick(t *testing.T) {
	c := newTestCoordinator(t)
	var fired int
	var mu sync.Mutex
	c.SetSessionsChangedNotifier(func(msg Message) {
		mu.Lock()
		fired++
		mu.Unlock()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx, 20*time.Millisecond)
	defer c.Shutdown()

	// Create a session so handleTick has something to broadcast about.
	if _, err := c.Create("/proj", "bash"); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Wait for at least one tick fan-out.
	deadline := time.Now().Add(time.Second)
	for {
		mu.Lock()
		n := fired
		mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("notifier never fired")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCoordinator_PreviewUnknownSessionReturnsError(t *testing.T) {
	c := newTestCoordinator(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx, 50*time.Millisecond)
	defer c.Shutdown()

	err := c.Preview("does-not-exist")
	if err == nil {
		t.Fatal("Preview should fail for unknown session")
	}
}

func TestCoordinator_StartIsIdempotent(t *testing.T) {
	c := newTestCoordinator(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Two Start calls must not spawn two run goroutines competing on
	// the same inbox. The second call is a no-op.
	c.Start(ctx, 50*time.Millisecond)
	c.Start(ctx, 50*time.Millisecond)
	defer c.Shutdown()

	// If two run goroutines were racing on inbox, this Create call
	// could be picked up by the wrong one and corrupt state. We just
	// verify it returns a valid id, which is enough to prove a single
	// well-formed actor processed it.
	id, err := c.Create("/proj", "bash")
	if err != nil {
		t.Fatalf("create after double Start: %v", err)
	}
	if id == "" {
		t.Fatal("Create returned empty id after double Start")
	}
}

func TestCoordinator_OperationsAfterShutdownReturnError(t *testing.T) {
	c := newTestCoordinator(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx, 50*time.Millisecond)
	c.Shutdown()

	if _, err := c.Create("/proj", "bash"); err != errCoordinatorStopped {
		t.Errorf("Create after Shutdown: err = %v, want errCoordinatorStopped", err)
	}
	if err := c.Stop("nonexistent"); err != errCoordinatorStopped {
		t.Errorf("Stop after Shutdown: err = %v, want errCoordinatorStopped", err)
	}
	if err := c.Preview("nonexistent"); err != errCoordinatorStopped {
		t.Errorf("Preview after Shutdown: err = %v, want errCoordinatorStopped", err)
	}
}

// TestCoordinator_TickFanOutIsAsync verifies that handleTickInternal
// returns immediately even when a Driver's Tick is slow — the async
// fan-out worker carries the load off the actor goroutine, so other
// inbox messages (e.g. AllSessionInfos called from a Server handler)
// can still be processed while ticks are gathering.
//
// The test installs a Driver whose Tick blocks on a gate, waits for
// the actor to actually call into Tick (so we know the fan-out is
// active), then checks that AllSessionInfos returns promptly. This is
// the regression test for the "Tick is synchronous on the actor
// goroutine" issue surfaced in self-review.
func TestCoordinator_TickFanOutIsAsync(t *testing.T) {
	gate := make(chan struct{})
	tickStarted := make(chan struct{}, 1)
	registry := driver.NewRegistry(slowFactory(gate, tickStarted))
	registry.Register("slow", slowFactory(gate, tickStarted))
	dataDir := t.TempDir()
	tmuxStub := &stubTmuxClient{}
	sessions := session.NewSessionService(tmuxStub, dataDir)
	drivers := driver.NewDriverService(registry, driver.Deps{})
	c := NewCoordinator(sessions, drivers, &stubPanes{}, nil, "roost", "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx, 20*time.Millisecond)
	defer func() {
		close(gate) // release any blocked Tick before shutting down
		c.Shutdown()
	}()

	if _, err := c.Create("/proj", "slow"); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Wait for the actor's ticker to fire, dispatch a fan-out, and
	// for the slow Driver's Tick to actually be entered.
	select {
	case <-tickStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("slow Tick was never invoked — fan-out not firing")
	}

	// At this point: the Coordinator actor has dispatched fanOutTicks
	// and returned from handleTickInternal. The fanOutTicks worker
	// goroutine is blocked on the slow Driver's Tick. The Coordinator
	// actor goroutine MUST still be free to process commands that do
	// not touch the slow Driver — ActiveWindowID is the cleanest probe
	// because it only reads Coordinator state.
	done := make(chan struct{})
	go func() {
		_ = c.ActiveWindowID()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Coordinator actor blocked while a slow Tick was in flight")
	}
}

// slowDriver is a Driver impl that blocks Tick on a shared gate
// channel and signals start via tickStarted. All other methods are
// no-ops. Used by TestCoordinator_TickFanOutIsAsync to simulate a
// Driver whose periodic poll takes a long time.
type slowDriver struct {
	gate        <-chan struct{}
	tickStarted chan<- struct{}
}

func slowFactory(gate <-chan struct{}, started chan<- struct{}) driver.Factory {
	return func(deps driver.Deps) driver.Driver {
		return &slowDriver{gate: gate, tickStarted: started}
	}
}

func (d *slowDriver) Name() string                       { return "slow" }
func (d *slowDriver) DisplayName() string                { return "slow" }
func (d *slowDriver) MarkSpawned()                       {}
func (d *slowDriver) Tick(time.Time, driver.WindowInfo) {
	// Best-effort signal: only the first Tick reports to keep the
	// channel small and avoid blocking subsequent ticks.
	select {
	case d.tickStarted <- struct{}{}:
	default:
	}
	<-d.gate
}
func (d *slowDriver) HandleEvent(driver.AgentEvent) bool { return false }
func (d *slowDriver) Close()                             {}
func (d *slowDriver) Status() (driver.StatusInfo, bool) {
	return driver.StatusInfo{Status: driver.StatusIdle}, true
}
func (d *slowDriver) View() driver.SessionView                { return driver.SessionView{} }
func (d *slowDriver) PersistedState() map[string]string       { return nil }
func (d *slowDriver) RestorePersistedState(map[string]string) {}
func (d *slowDriver) SpawnCommand(base string) string         { return base }
func (d *slowDriver) Atomic(fn func(driver.Driver))           { fn(d) }

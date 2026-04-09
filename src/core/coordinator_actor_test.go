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
	return wid, nil
}

func (c *stubTmuxClient) KillWindow(windowID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.killed++
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

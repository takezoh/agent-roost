package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/takezoh/agent-roost/driver"
	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/state"
)

func TestMain(m *testing.M) {
	// Register drivers so reducers can resolve commands. The runtime
	// tests don't exercise driver-specific behaviour — they just need
	// SOMETHING in the registry.
	state.Register(driver.NewGenericDriver("", 0))
	state.Register(driver.NewGenericDriver("shell", 0))
	state.Register(driver.NewCodexDriver(""))
	os.Exit(m.Run())
}

// === Fake backends for runtime tests ===

type fakeTmuxBackend struct {
	mu               sync.Mutex
	spawnCalls       int
	spawnCmds        []string
	killCalls        int
	sessionKillCalls int
	killedPanes      []string
	breakCalls       int
	breakTargets     []string
	breakNewCalls    int
	breakNewNames    []string
	joinCalls        int
	joinSources      []string
	joinTargets      []string
	swapCalls        int
	swapSources      []string
	swapTargets      []string
	resizeCalls      int
	resizeTargets    []string
	resizeWidths     []int
	resizeHeights    []int
	respawnCmds      []string
	terminatedPanes  []string
	statusLines      []string
	envs             map[string]string
	popups           []string
	alive            map[string]bool
	captured         string
	inspectCalls     []string
	inspectSnapshot  PaneSnapshot
	spawnWID         string
	spawnPane        string
	breakNewWID      string
	spawnErr         error
	swapErr          error
	envOutput        string
	paneWidth        int
	paneHeight       int
	paneIDs          map[string]string
}

func newFakeTmux() *fakeTmuxBackend {
	return &fakeTmuxBackend{
		alive:       map[string]bool{},
		envs:        map[string]string{},
		paneIDs:     map[string]string{},
		spawnWID:    "1",
		spawnPane:   "%1",
		breakNewWID: "9",
		paneWidth:   120,
		paneHeight:  40,
	}
}

func (f *fakeTmuxBackend) SpawnWindow(name, command, startDir string, env map[string]string) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spawnCalls++
	f.spawnCmds = append(f.spawnCmds, command)
	if f.spawnErr != nil {
		return "", "", f.spawnErr
	}
	return f.spawnWID, f.spawnPane, nil
}

func (f *fakeTmuxBackend) ShowEnvironment() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.envOutput, nil
}

func (f *fakeTmuxBackend) KillPaneWindow(paneID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.killCalls++
	f.killedPanes = append(f.killedPanes, paneID)
	return nil
}

func (f *fakeTmuxBackend) TerminatePane(paneID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.terminatedPanes = append(f.terminatedPanes, paneID)
	return nil
}

func (f *fakeTmuxBackend) RunChain(ops ...[]string) error {
	return nil
}
func (f *fakeTmuxBackend) BreakPane(srcPane, dstWindow string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.breakCalls++
	f.breakTargets = append(f.breakTargets, dstWindow)
	return nil
}
func (f *fakeTmuxBackend) SwapPane(srcPane, dstPane string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.swapCalls++
	f.swapSources = append(f.swapSources, srcPane)
	f.swapTargets = append(f.swapTargets, dstPane)
	return f.swapErr
}
func (f *fakeTmuxBackend) BreakPaneToNewWindow(srcPane, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.breakNewCalls++
	f.breakNewNames = append(f.breakNewNames, name)
	return f.breakNewWID, nil
}
func (f *fakeTmuxBackend) JoinPane(srcPane, dstPane string, before bool, sizePct int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.joinCalls++
	f.joinSources = append(f.joinSources, srcPane)
	f.joinTargets = append(f.joinTargets, dstPane)
	return nil
}
func (f *fakeTmuxBackend) PaneID(target string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	lookup := strings.Replace(target, ":=", ":", 1)
	if id, ok := f.paneIDs[lookup]; ok {
		if id == "error" {
			return "", fmt.Errorf("tmux error for %s", target)
		}
		return id, nil
	}
	if target == "roost-test:0.0" && f.spawnPane != "" {
		return f.spawnPane, nil
	}
	return "%main", nil
}
func (f *fakeTmuxBackend) PaneSize(string) (int, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.paneWidth, f.paneHeight, nil
}
func (f *fakeTmuxBackend) SelectPane(string) error { return nil }
func (f *fakeTmuxBackend) ResizeWindow(target string, width, height int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resizeCalls++
	f.resizeTargets = append(f.resizeTargets, target)
	f.resizeWidths = append(f.resizeWidths, width)
	f.resizeHeights = append(f.resizeHeights, height)
	return nil
}
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
func (f *fakeTmuxBackend) InspectPane(target string, _ int) (PaneSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inspectCalls = append(f.inspectCalls, target)
	snap := f.inspectSnapshot
	snap.Target = target
	return snap, nil
}
func (f *fakeTmuxBackend) DetachClient() error { return nil }
func (f *fakeTmuxBackend) KillSession() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessionKillCalls++
	return nil
}
func (f *fakeTmuxBackend) DisplayPopup(w, h, cmd string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.popups = append(f.popups, cmd)
	return nil
}

type recordingPersist struct {
	mu    sync.Mutex
	saves int
	last  []SessionSnapshot
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

type recordingWatcher struct {
	mu      sync.Mutex
	watches map[state.SessionID]string
}

func (r *recordingWatcher) Watch(sessionID state.SessionID, path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.watches == nil {
		r.watches = map[state.SessionID]string{}
	}
	r.watches[sessionID] = path
	return nil
}

func (r *recordingWatcher) Unwatch(sessionID state.SessionID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.watches, sessionID)
	return nil
}

func (r *recordingWatcher) Events() <-chan FSEvent { return nil }
func (r *recordingWatcher) Close() error           { return nil }

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

func TestExecuteKillSession(t *testing.T) {
	tmux := newFakeTmux()
	r := New(Config{
		SessionName: "roost-test",
		RoostExe:    "/usr/local/bin/roost",
		Tmux:        tmux,
	})

	r.execute(state.EffKillSession{})

	tmux.mu.Lock()
	defer tmux.mu.Unlock()
	if tmux.sessionKillCalls != 1 {
		t.Fatalf("sessionKillCalls = %d, want 1", tmux.sessionKillCalls)
	}
}

func TestSendResponseSyncFlushesImmediately(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	r := New(Config{
		SessionName: "roost-test",
		RoostExe:    "/usr/local/bin/roost",
	})
	cc := newIPCConn(1, server)
	r.conns[1] = cc

	done := make(chan []byte, 1)
	go func() {
		reader := bufio.NewReader(client)
		line, _ := reader.ReadBytes('\n')
		done <- line
	}()

	r.execute(state.EffSendResponseSync{
		ConnID: 1,
		ReqID:  "req-1",
		Body:   nil,
	})

	select {
	case line := <-done:
		env, err := proto.DecodeEnvelope(line)
		if err != nil {
			t.Fatalf("DecodeEnvelope: %v", err)
		}
		if env.Type != proto.TypeResponse {
			t.Fatalf("type = %q, want %q", env.Type, proto.TypeResponse)
		}
		if env.ReqID != "req-1" {
			t.Fatalf("req_id = %q, want req-1", env.ReqID)
		}
		if env.Status != proto.StatusOK {
			t.Fatalf("status = %q, want %q", env.Status, proto.StatusOK)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for sync response")
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

	r.Enqueue(state.EvEvent{
		ConnID: 1, ReqID: "r1", Event: "create-session",
		Payload: json.RawMessage(`{"project":"/tmp/test","command":"stub-fallback"}`),
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
	if tmux.resizeCalls == 0 {
		t.Error("expected spawned window to be resized to main pane size")
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

func TestActivateSessionInspectsPanesAroundSwap(t *testing.T) {
	tmux := newFakeTmux()
	r := New(Config{
		SessionName:       "roost-test",
		MainPaneHeightPct: 70,
		Tmux:              tmux,
	})
	r.sessionPanes["_main"] = "%main"
	r.sessionPanes["sess-1"] = "%3"

	r.execute(state.EffActivateSession{
		SessionID: "sess-1",
		Reason:    state.EventPreviewSession,
	})

	tmux.mu.Lock()
	defer tmux.mu.Unlock()
	if tmux.swapCalls != 1 {
		t.Fatalf("swapCalls = %d, want 1", tmux.swapCalls)
	}
	if tmux.swapSources[0] != "%3" || tmux.swapTargets[0] != "roost-test:0.0" {
		t.Fatalf("swap = %q -> %q, want %%3 -> roost-test:0.0", tmux.swapSources[0], tmux.swapTargets[0])
	}
	if r.sessionPanes["sess-1"] != "%3" {
		t.Errorf("sessionPanes[sess-1] = %q, want %%3", r.sessionPanes["sess-1"])
	}
	if r.sessionPanes["_main"] != "%main" {
		t.Errorf("sessionPanes[_main] = %q, want %%main", r.sessionPanes["_main"])
	}

	if len(tmux.inspectCalls) != 3 {
		t.Fatalf("inspectCalls = %d, want 3", len(tmux.inspectCalls))
	}
	wantTargets := []string{"roost-test:0.0", "%3", "roost-test:0.0"}
	for i, want := range wantTargets {
		if tmux.inspectCalls[i] != want {
			t.Fatalf("inspectCalls[%d] = %q, want %q", i, tmux.inspectCalls[i], want)
		}
	}
	if r.activeSession != "sess-1" {
		t.Fatalf("activeSession = %q, want sess-1", r.activeSession)
	}
}

func TestActivateSessionInitializesMainPaneIDOnDemand(t *testing.T) {
	tmux := newFakeTmux()
	r := New(Config{
		SessionName:       "roost-test",
		MainPaneHeightPct: 70,
		Tmux:              tmux,
	})
	r.sessionPanes["sess-1"] = "%3"

	r.execute(state.EffActivateSession{
		SessionID: "sess-1",
		Reason:    state.EventPreviewSession,
	})

	if r.sessionPanes["_main"] != "%1" {
		t.Fatalf("sessionPanes[_main] = %q, want %%1", r.sessionPanes["_main"])
	}
	tmux.mu.Lock()
	defer tmux.mu.Unlock()
	if tmux.envs["ROOST_SESSION__main"] != "%1" {
		t.Fatalf("ROOST_SESSION__main = %q, want %%1", tmux.envs["ROOST_SESSION__main"])
	}
	if tmux.swapCalls != 1 {
		t.Fatalf("swapCalls = %d, want 1", tmux.swapCalls)
	}
}

func TestTerminateSessionSendsTerminateKey(t *testing.T) {
	tmux := newFakeTmux()
	r := New(Config{
		SessionName:       "roost-test",
		MainPaneHeightPct: 70,
		Tmux:              tmux,
	})
	r.sessionPanes["sess-1"] = "%3"

	r.execute(state.EffTerminateSession{SessionID: "sess-1"})

	tmux.mu.Lock()
	defer tmux.mu.Unlock()
	if len(tmux.terminatedPanes) != 1 || tmux.terminatedPanes[0] != "%3" {
		t.Fatalf("terminatedPanes = %#v, want %%3", tmux.terminatedPanes)
	}
}

func TestActivateSessionMissingPaneEnqueuesWindowVanished(t *testing.T) {
	tmux := newFakeTmux()
	tmux.swapErr = fmt.Errorf("tmux swap-pane -d -s %%3 -t roost-test:0.0: exit status 1: can't find pane: %%3")
	r := New(Config{
		SessionName:       "roost-test",
		MainPaneHeightPct: 70,
		Tmux:              tmux,
	})
	r.sessionPanes["_main"] = "%main"
	r.sessionPanes["sess-1"] = "%3"

	r.execute(state.EffActivateSession{
		SessionID: "sess-1",
		Reason:    state.EventPreviewSession,
	})

	select {
	case ev := <-r.eventCh:
		v, ok := ev.(state.EvTmuxWindowVanished)
		if !ok {
			t.Fatalf("event type = %T, want EvTmuxWindowVanished", ev)
		}
		if v.SessionID != "sess-1" {
			t.Fatalf("SessionID = %q, want sess-1", v.SessionID)
		}
	case <-time.After(time.Second):
		t.Fatal("expected EvTmuxWindowVanished")
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

func TestCommandToStateEvent(t *testing.T) {
	cases := []struct {
		cmd  proto.Command
		want string
	}{
		{proto.CmdSubscribe{}, "EvCmdSubscribe"},
		{proto.CmdEvent{Event: "test"}, "EvEvent"},
	}
	for _, c := range cases {
		ev := commandToStateEvent(state.ConnID(1), "r1", c.cmd)
		if ev == nil {
			t.Errorf("nil event for %T", c.cmd)
		}
	}
}

func TestEventTypeName(t *testing.T) {
	cases := []struct {
		ev   state.Event
		want string
	}{
		{state.EvTick{}, "EvTick"},
		{state.EvEvent{}, "EvEvent"},
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
		ID: "abc", Command: "stub-x",
	}
	r.sessionPanes["abc"] = "%5"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = r.Run(ctx) }()

	r.Enqueue(state.EvEvent{ConnID: 1, ReqID: "r", Event: "stop-session", Payload: json.RawMessage(`{"session_id":"abc"}`)})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		tmux.mu.Lock()
		n := len(tmux.terminatedPanes)
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
	if len(tmux.terminatedPanes) != 1 || tmux.terminatedPanes[0] != "%5" {
		t.Errorf("terminatedPanes = %#v, want [%%5]", tmux.terminatedPanes)
	}
}

func TestIsShellCommand(t *testing.T) {
	if !isShellCommand("shell") {
		t.Error("expected true for 'shell'")
	}
	if isShellCommand("claude") {
		t.Error("expected false for 'claude'")
	}
	if isShellCommand("") {
		t.Error("expected false for empty")
	}
}

func TestRuntimeShellSessionSpawnsWithoutCommand(t *testing.T) {
	tmux := newFakeTmux()
	persist := &recordingPersist{}
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         tmux,
		Persist:      persist,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = r.Run(ctx) }()

	r.Enqueue(state.EvEvent{
		ConnID: 1, ReqID: "r1", Event: "create-session",
		Payload: json.RawMessage(`{"project":"/tmp/test","command":"shell"}`),
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tmux.mu.Lock()
		spawned := tmux.spawnCalls
		tmux.mu.Unlock()
		if spawned >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-r.Done()

	tmux.mu.Lock()
	defer tmux.mu.Unlock()
	if tmux.spawnCalls != 1 {
		t.Fatalf("spawnCalls = %d, want 1", tmux.spawnCalls)
	}
	if tmux.spawnCmds[0] != "" {
		t.Errorf("spawn command = %q, want empty (login shell)", tmux.spawnCmds[0])
	}
}

func TestRecreateAllUsesPrepareLaunch(t *testing.T) {
	tmux := newFakeTmux()
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         tmux,
		Persist:      &recordingPersist{},
	})
	drv := state.GetDriver("codex")
	ds := drv.NewState(time.Now()).(driver.CodexState)
	ds.CodexSessionID = "resume-123"
	ds.ManagedWorkingDir = "/repo/.roost/worktrees/example"
	ds.WorkingDir = "/repo/.roost/worktrees/example"
	r.state.Sessions[state.SessionID("s1")] = state.Session{
		ID:      state.SessionID("s1"),
		Project: "/repo",
		Command: "codex --worktree example --model gpt-5-codex",
		Driver:  ds,
	}

	if err := r.RecreateAll(); err != nil {
		t.Fatalf("RecreateAll error: %v", err)
	}

	tmux.mu.Lock()
	defer tmux.mu.Unlock()
	if len(tmux.spawnCmds) != 1 {
		t.Fatalf("spawnCmds = %d, want 1", len(tmux.spawnCmds))
	}
	if tmux.spawnCmds[0] != "exec codex --model gpt-5-codex resume resume-123" {
		t.Fatalf("spawnCmd = %q", tmux.spawnCmds[0])
	}
}

func TestSpawnTmuxWindowAsyncUsesPrepareLaunch(t *testing.T) {
	tmux := newFakeTmux()
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         tmux,
		Persist:      &recordingPersist{},
	})
	drv := state.GetDriver("codex")
	ds := drv.NewState(time.Now()).(driver.CodexState)
	ds.ManagedWorkingDir = "/repo/.roost/worktrees/example"
	ds.WorkingDir = "/repo/.roost/worktrees/example"
	r.state.Sessions[state.SessionID("s1")] = state.Session{
		ID:      state.SessionID("s1"),
		Project: "/repo",
		Command: "codex --worktree example --model gpt-5-codex",
		Driver:  ds,
	}

	r.spawnTmuxWindowAsync(state.EffSpawnTmuxWindow{
		SessionID: state.SessionID("s1"),
		Mode:      state.LaunchModeCreate,
		Project:   "/repo",
		Env:       map[string]string{"ROOST_SESSION_ID": "s1"},
	})

	tmux.mu.Lock()
	defer tmux.mu.Unlock()
	if len(tmux.spawnCmds) != 1 {
		t.Fatalf("spawnCmds = %d, want 1", len(tmux.spawnCmds))
	}
	if tmux.spawnCmds[0] != "exec codex --model gpt-5-codex" {
		t.Fatalf("spawnCmd = %q", tmux.spawnCmds[0])
	}
}

func TestReconcileDetectsVanishedPane(t *testing.T) {
	ftmux := newFakeTmux()
	ftmux.alive["%3"] = false
	ftmux.envs["ROOST_SESSION_tracked1"] = "%3"
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 20 * time.Millisecond,
		Tmux:         ftmux,
	})
	drv := state.GetDriver("shell")
	r.state.Sessions[state.SessionID("tracked1")] = state.Session{
		ID:      state.SessionID("tracked1"),
		Command: "shell",
		Driver:  drv.NewState(time.Now()),
	}
	r.sessionPanes[state.SessionID("tracked1")] = "%3"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = r.Run(ctx) }()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		ftmux.mu.Lock()
		_, stillSet := ftmux.envs["ROOST_SESSION_tracked1"]
		ftmux.mu.Unlock()
		if !stillSet {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-r.Done()

	ftmux.mu.Lock()
	defer ftmux.mu.Unlock()
	if _, ok := ftmux.envs["ROOST_SESSION_tracked1"]; ok {
		t.Error("ROOST_SESSION_tracked1 should be unset after pane vanished")
	}
}

func TestReconcileSkipsWithoutTrackedPanes(t *testing.T) {
	ftmux := newFakeTmux()
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 20 * time.Millisecond,
		Tmux:         ftmux,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = r.Run(ctx) }()

	time.Sleep(60 * time.Millisecond)
	cancel()
	<-r.Done()

	ftmux.mu.Lock()
	defer ftmux.mu.Unlock()
	if ftmux.killCalls != 0 {
		t.Errorf("killCalls = %d, want 0 (no orphans)", ftmux.killCalls)
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

func TestMonitorParkedPanesTracksInactiveOnly(t *testing.T) {
	tmux := newFakeTmux()
	tmux.inspectSnapshot = PaneSnapshot{
		Target:         "%2",
		CurrentCommand: "node",
		CursorX:        "0",
		CursorY:        "35",
		ContentTail:    "gemini",
	}
	r := New(Config{
		SessionName: "roost-test",
		Tmux:        tmux,
	})
	r.sessionPanes["active"] = "%1"
	r.sessionPanes["idle"] = "%2"
	r.activeSession = "active"

	r.monitorParkedPanes()

	if _, ok := r.parkedPaneSnapshot["active"]; ok {
		t.Fatal("active session should not be tracked as parked")
	}
	if _, ok := r.parkedPaneSnapshot["idle"]; !ok {
		t.Fatal("inactive session should be tracked as parked")
	}
}

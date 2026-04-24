// Package runtime is the imperative shell for the pure state package.
// It owns the single event loop goroutine, the worker pool, the IPC
// server, the fsnotify watcher, and the tmux backend. Every state
// mutation goes through state.Reduce; every side effect is dispatched
// through the Effect interpreter in interpret.go.
//
// The event loop is the only goroutine that touches Runtime.state.
// Workers, IPC readers, and the fsnotify watcher feed events back via
// channels — they never read or write state directly.
package runtime

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/features"
	"github.com/takezoh/agent-roost/runtime/worker"
	"github.com/takezoh/agent-roost/state"
)

// Config carries the runtime's startup parameters. Backends are
// injected (interfaces) so tests can swap fakes.
type Config struct {
	SessionName       string
	RoostExe          string
	DataDir           string
	TickInterval      time.Duration
	FastTickInterval  time.Duration // default 100ms; fast-detects active-frame pane death.
	Workers           int
	MainPaneHeightPct int

	Tmux     TmuxBackend
	Persist  PersistBackend
	EventLog EventLogBackend
	ToolLog  ToolLogBackend
	Watcher  FSWatcher
	Pool     *worker.Pool
	Notifier Notifier

	// TerminalEvict is called with the pane target string whenever a session
	// pane is unregistered. It should release the VT emulator held for that
	// pane to prevent unbounded memory growth. May be nil.
	TerminalEvict func(pane string)

	// Tap, if non-nil, is used to attach a raw byte stream reader to each
	// frame's pane. The reader feeds a per-frame vt.Terminal and emits
	// EvPaneOsc events for OSC 9/99/777 notifications detected in the stream.
	Tap PaneTap

	// Features is the set of runtime flags built from the config file.
	// Injected into state.State once at construction; never mutated.
	Features features.Set

	// Launcher wraps agent launch plans before they reach tmux, enabling
	// sandbox implementations. nil falls back to DirectLauncher (no-op).
	Launcher AgentLauncher
}

// Runtime owns the event loop goroutine and the side-effect backends.
// All fields are read/written from the event loop goroutine alone
// except where noted.
type Runtime struct {
	cfg Config

	state state.State

	// sessionPanes maps each FrameID to its tmux pane id ("%5", "%12", ...).
	sessionPanes map[state.FrameID]string
	// mainPaneSession is the SessionID whose frame is currently in pane 0.1,
	// or "". Distinct from state.ActiveSession (logical focus): this tracks
	// the physical occupant only.
	mainPaneSession state.SessionID
	activeFrameID   state.FrameID
	// parkedPaneSnapshot stores the last logged parked-pane signature per session.
	parkedPaneSnapshot map[state.FrameID]string

	eventCh    chan state.Event   // public events from any goroutine
	internalCh chan internalEvent // runtime-internal lifecycle (conn open/close)

	workers *worker.Pool

	relay *FileRelay

	listener net.Listener
	conns    map[state.ConnID]*ipcConn // owned by event loop
	nextConn state.ConnID              // owned by event loop

	done chan struct{}

	taps *tapManager

	// fastProbeInFlight guards against spawning multiple concurrent
	// PaneAlive probes from the fastTicker. Written from any goroutine,
	// read from the event loop.
	fastProbeInFlight atomic.Bool

	// tickN is a monotonic counter incremented on each main tick, passed
	// as EvTick.N so reducers and drivers can gate work to every N-th tick.
	tickN uint64
	// parkedScanIdx is a round-robin cursor for monitorParkedPanes;
	// one non-active frame is inspected per tick rather than all of them.
	parkedScanIdx int

	// workspaceResolver resolves the workspace name for each session's
	// project directory, with mtime-based caching of .roost/settings.toml.
	workspaceResolver *config.WorkspaceResolver

	// frameCleanups holds WrappedLaunch.Cleanup callbacks keyed by FrameID.
	// Protected by frameCleanupsMu because storeFrameCleanup is called from
	// goroutines (spawnTmuxWindowAsync) while invoke/drain run in the event loop.
	frameCleanupsMu sync.Mutex
	frameCleanups   map[state.FrameID]func() error
}

// New constructs a Runtime ready for Run. Backends must be set on the
// Config; missing backends are stubbed with no-ops at construction so
// the loop can start even if the caller has not wired everything yet
// (useful for incremental tests).
func New(cfg Config) *Runtime {
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.TickInterval <= 0 {
		cfg.TickInterval = time.Second
	}
	if cfg.FastTickInterval <= 0 {
		cfg.FastTickInterval = 100 * time.Millisecond
	}
	if cfg.MainPaneHeightPct <= 0 {
		cfg.MainPaneHeightPct = 70
	}
	if cfg.Tmux == nil {
		cfg.Tmux = noopTmux{}
	}
	if cfg.Persist == nil {
		cfg.Persist = noopPersist{}
	}
	if cfg.EventLog == nil {
		cfg.EventLog = noopEventLog{}
	}
	if cfg.Watcher == nil {
		cfg.Watcher = noopWatcher{}
	}
	if cfg.ToolLog == nil {
		cfg.ToolLog = noopToolLog{}
	}
	if cfg.Notifier == nil {
		cfg.Notifier = noopNotifier{}
	}
	initial := state.New()
	initial.Features = cfg.Features
	r := &Runtime{
		cfg:                cfg,
		state:              initial,
		sessionPanes:       map[state.FrameID]string{},
		parkedPaneSnapshot: map[state.FrameID]string{},
		eventCh:            make(chan state.Event, 256),
		internalCh:         make(chan internalEvent, 64),
		conns:              map[state.ConnID]*ipcConn{},
		done:               make(chan struct{}),
		workspaceResolver:  config.NewWorkspaceResolver(),
		frameCleanups:      map[state.FrameID]func() error{},
	}
	if cfg.Pool != nil {
		r.workers = cfg.Pool
	} else {
		r.workers = worker.NewPool(context.Background(), cfg.Workers)
	}
	return r
}

// Done signals when Run has fully exited.
func (r *Runtime) Done() <-chan struct{} { return r.done }

// KnownProjects returns the canonical project paths for all sessions currently
// loaded in state. Must be called before Run starts (or from the event loop).
func (r *Runtime) KnownProjects() []string {
	seen := make(map[string]struct{})
	var out []string
	for _, sess := range r.state.Sessions {
		if sess.Project != "" {
			if _, ok := seen[sess.Project]; !ok {
				seen[sess.Project] = struct{}{}
				out = append(out, sess.Project)
			}
		}
	}
	return out
}

// Enqueue submits an event into the loop from outside. The runtime
// itself uses the same channel from inside the loop for self-events.
// Safe to call from any goroutine.
func (r *Runtime) Enqueue(ev state.Event) {
	select {
	case r.eventCh <- ev:
	default:
		slog.Warn("runtime: event channel full, dropping", "type", eventTypeName(ev))
	}
}

// SetRelay registers a FileRelay with the runtime via the event loop.
func (r *Runtime) SetRelay(fr *FileRelay) {
	r.internalCh <- internalSetRelay{relay: fr}
}

// StartTapsForRestoredFrames attaches a pane tap to each frame that was
// restored from the snapshot.  Normal sessions route through
// EvTmuxPaneSpawned → EffRegisterPane → tapManager.start, but bootstrap
// paths (warm restart, cold-start RecreateAll) populate sessionPanes
// directly without emitting that effect, leaving restored frames
// without a tap.  Call once from main.go after Run has been started.
func (r *Runtime) StartTapsForRestoredFrames() {
	r.enqueueInternal(internalStartRestoredTaps{})
}

// Run is the event loop. It blocks until ctx is cancelled.
//
// Internal events (connOpen, connClose) bypass state.Reduce and go
// straight to dispatchInternal — they manipulate runtime fields the
// reducer can't see (the conns map, the next conn id counter).
func (r *Runtime) Run(ctx context.Context) error { //nolint:funlen
	defer close(r.done)
	defer r.workers.Stop()
	defer r.shutdownIPC()
	defer r.cfg.EventLog.CloseAll()
	defer r.cfg.ToolLog.CloseAll()
	defer r.deactivateBeforeExit()
	// Sandbox resources are released via state.EffReleaseFrameSandboxes on
	// explicit shutdown. On daemon crash (SIGKILL) or panic, the defer stack
	// does not run; next startup's PruneOrphans recovers orphaned containers.

	r.taps = newTapManager(ctx, r.cfg.Tap)
	defer r.taps.stopAll()

	ticker := time.NewTicker(r.cfg.TickInterval)
	defer ticker.Stop()
	fastTicker := time.NewTicker(r.cfg.FastTickInterval)
	defer fastTicker.Stop()

	slog.Info("runtime: event loop started",
		"tick", r.cfg.TickInterval,
		"workers", r.cfg.Workers)

	for {
		select {
		case <-ctx.Done():
			slog.Info("runtime: event loop stopping (ctx done)")
			return ctx.Err()

		case ev, ok := <-r.eventCh:
			if !ok {
				return nil
			}
			r.dispatch(ev)

		case iev := <-r.internalCh:
			r.dispatchInternal(iev)

		case t := <-ticker.C:
			r.tickN++
			r.monitorParkedPanes()
			r.dispatch(state.EvTick{Now: t, PaneTargets: r.snapshotPaneTargets(), N: r.tickN})

		case <-fastTicker.C:
			r.scheduleActiveFramePaneProbe()

		case res := <-r.workers.Results():
			r.dispatch(res)

		case fsev := <-r.cfg.Watcher.Events():
			r.dispatch(state.EvFileChanged{
				FrameID: fsev.FrameID,
				Path:    fsev.Path,
			})
		}
	}
}

// scheduleActiveFramePaneProbe は active frame (pane 0.1 にスワップ中) の
// 死亡を高速検出する。PaneAlive の tmux shell-out をゴルーチンに委譲して
// event loop をブロックしない。同時実行は atomic guard で 1 本に制限する。
func (r *Runtime) scheduleActiveFramePaneProbe() {
	if r.activeFrameID == "" {
		return
	}
	if !r.fastProbeInFlight.CompareAndSwap(false, true) {
		return
	}
	target := substitutePlaceholdersString("{sessionName}:0.1", r.cfg.SessionName, r.cfg.RoostExe)
	frameID := r.activeFrameID // snapshot owned by event loop goroutine
	go func() {
		defer r.fastProbeInFlight.Store(false)
		alive, err := r.cfg.Tmux.PaneAlive(target)
		if err != nil || alive {
			return
		}
		r.Enqueue(state.EvPaneDied{
			Pane:         "{sessionName}:0.1",
			OwnerFrameID: frameID,
		})
	}()
}

// dispatch runs Reduce against the current state and executes every
// resulting effect. Effects may enqueue more events into r.eventCh
// (e.g. tmux spawn → EvTmuxWindowSpawned), which are picked up on
// subsequent loop iterations.
func (r *Runtime) dispatch(ev state.Event) {
	next, effects := state.Reduce(r.state, ev)
	r.state = next
	for _, eff := range effects {
		r.execute(eff)
	}
}

// snapshotPaneTargets returns a copy of sessionPanes for inclusion in
// EvTick so reducers can forward pane targets to drivers without
// accessing the runtime directly.
func (r *Runtime) snapshotPaneTargets() map[state.SessionID]string {
	if len(r.sessionPanes) == 0 {
		return nil
	}
	out := make(map[state.SessionID]string, len(r.sessionPanes))
	for k, v := range r.sessionPanes {
		out[state.SessionID(k)] = v
	}
	return out
}

// sessionPaneForSession returns the pane target for the active frame of the
// given session. Returns "" if the session has no registered pane.
func (r *Runtime) sessionPaneForSession(sid state.SessionID) string {
	sess, ok := r.state.Sessions[sid]
	if !ok {
		return ""
	}
	for _, fr := range sess.Frames {
		if p, ok := r.sessionPanes[fr.ID]; ok && p != "" {
			return p
		}
	}
	return ""
}

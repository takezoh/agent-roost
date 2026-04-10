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
	"errors"
	"log/slog"
	"net"
	"time"

	"github.com/take/agent-roost/runtime/worker"
	"github.com/take/agent-roost/state"
)

// Config carries the runtime's startup parameters. Backends are
// injected (interfaces) so tests can swap fakes.
type Config struct {
	SessionName  string
	RoostExe     string
	DataDir      string
	TickInterval time.Duration
	Workers      int

	Tmux     TmuxBackend
	Persist  PersistBackend
	EventLog EventLogBackend
	Watcher  FSWatcher
	Pool     *worker.Pool
	Runners  *worker.Runners
}

// Runtime owns the event loop goroutine and the side-effect backends.
// All fields are read/written from the event loop goroutine alone
// except where noted.
type Runtime struct {
	cfg Config

	state state.State

	eventCh    chan state.Event   // public events from any goroutine
	internalCh chan internalEvent // runtime-internal lifecycle (conn open/close)

	workers *worker.Pool
	runners worker.Runners

	listener net.Listener
	conns    map[state.ConnID]*ipcConn // owned by event loop
	nextConn state.ConnID              // owned by event loop

	done chan struct{}
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
	r := &Runtime{
		cfg:        cfg,
		state:      state.New(),
		eventCh:    make(chan state.Event, 256),
		internalCh: make(chan internalEvent, 64),
		conns:      map[state.ConnID]*ipcConn{},
		done:       make(chan struct{}),
	}
	if cfg.Pool != nil {
		r.workers = cfg.Pool
	} else {
		r.workers = worker.NewPool(cfg.Workers)
	}
	if cfg.Runners != nil {
		r.runners = *cfg.Runners
	}
	return r
}

// ShutdownRequested reports whether a shutdown command was processed
// by the event loop. Safe to call after Run has exited (no concurrent
// writer once the loop is stopped).
func (r *Runtime) ShutdownRequested() bool {
	return r.state.ShutdownReq
}

// Done signals when Run has fully exited.
func (r *Runtime) Done() <-chan struct{} { return r.done }

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

// Run is the event loop. It blocks until ctx is cancelled.
//
// Internal events (connOpen, connClose) bypass state.Reduce and go
// straight to dispatchInternal — they manipulate runtime fields the
// reducer can't see (the conns map, the next conn id counter).
func (r *Runtime) Run(ctx context.Context) error {
	defer close(r.done)
	defer r.workers.Stop()
	defer r.shutdownIPC()
	defer r.cfg.EventLog.CloseAll()

	ticker := time.NewTicker(r.cfg.TickInterval)
	defer ticker.Stop()

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
			r.dispatch(state.EvTick{Now: t})

		case res := <-r.workers.Results():
			r.dispatch(res)

		case fsev := <-r.cfg.Watcher.Events():
			r.dispatch(state.EvTranscriptChanged{
				SessionID: state.SessionID(fsev.SessionID),
				Path:      fsev.Path,
			})
		}
	}
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

// errClosed is returned when the runtime has already shut down.
var errClosed = errors.New("runtime: closed")

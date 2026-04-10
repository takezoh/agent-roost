package runtime

import (
	"github.com/take/agent-roost/state"
	"github.com/take/agent-roost/driver"
)

// Backend interfaces. The runtime depends on these abstractions, not
// on concrete tmux/persistence/fs/log libraries, so tests can plug in
// fakes and so the production wiring lives in one place (cmd/main).

// TmuxBackend is the subset of tmux operations the runtime needs.
// Methods that return data are synchronous (the runtime calls them
// from execute() and waits for the result before queueing the
// follow-up event).
type TmuxBackend interface {
	// SpawnWindow creates a new tmux window for a session. Returns the
	// fresh window id and the pane id.
	SpawnWindow(name, command, startDir string, env map[string]string) (windowID, paneID string, err error)

	// KillWindow destroys a tmux window.
	KillWindow(windowID string) error

	// RunChain executes a sequence of swap-pane (or other) commands as
	// a single tmux invocation. Used for the swap-pane preview chain.
	RunChain(ops ...[]string) error

	// SelectPane focuses a tmux pane.
	SelectPane(target string) error

	// SetStatusLine writes the tmux status-left.
	SetStatusLine(line string) error

	// SetEnv writes a tmux session-level environment variable.
	SetEnv(key, value string) error
	// UnsetEnv removes a tmux session-level env var.
	UnsetEnv(key string) error

	// PaneAlive returns true if the named pane is currently alive
	// (i.e. #{pane_dead} == 0). False on error or dead pane.
	PaneAlive(target string) (bool, error)

	// RespawnPane runs respawn-pane against a dead pane.
	RespawnPane(target, command string) error

	// CapturePane returns the trailing nLines of a window's primary
	// pane content. Used by polling drivers via the worker pool.
	CapturePane(windowID string, nLines int) (string, error)

	// DetachClient detaches the current tmux client (used by Detach /
	// Shutdown commands).
	DetachClient() error

	// KillSession destroys the entire roost tmux session.
	KillSession() error

	// DisplayPopup runs `tmux display-popup`.
	DisplayPopup(width, height, cmd string) error
}

// PersistBackend abstracts sessions.json persistence so tests don't
// touch the filesystem.
type PersistBackend interface {
	Save(sessions []SessionSnapshot) error
	Load() ([]SessionSnapshot, error)
}

// SessionSnapshot is the on-disk format for one session in
// sessions.json. Includes the static metadata + the driver's persisted
// bag (opaque map of strings).
type SessionSnapshot struct {
	ID          string            `json:"id"`
	Project     string            `json:"project"`
	Command     string            `json:"command"`
	WindowID    string            `json:"window_id"`
	PaneID string            `json:"pane_id"`
	CreatedAt   string            `json:"created_at"`
	Driver      string            `json:"driver"`
	DriverState map[string]string `json:"driver_state"`
}

// EventLogBackend writes per-session event log lines. The
// implementation lazily opens the file on first append and keeps it
// open until Close(sessionID) is called.
type EventLogBackend interface {
	Append(sessionID state.SessionID, line string) error
	Close(sessionID state.SessionID)
	CloseAll()
}

// FSWatcher is the fsnotify wrapper. It watches per-session
// transcript files and emits FSEvent values on Events() when they
// change.
type FSWatcher interface {
	Watch(sessionID state.SessionID, path string) error
	Unwatch(sessionID state.SessionID) error
	Events() <-chan FSEvent
	Close() error
}

// FSEvent is the runtime-side representation of a transcript file
// change. SessionID is set by the watcher (which knows the path →
// session mapping it was given via Watch).
type FSEvent struct {
	SessionID state.SessionID
	Path      string
}

// === noop backends (used until production wiring lands) ===

type noopTmux struct{}

func (noopTmux) SpawnWindow(name, command, startDir string, env map[string]string) (string, string, error) {
	return "", "", nil
}
func (noopTmux) KillWindow(string) error                  { return nil }
func (noopTmux) RunChain(...[]string) error               { return nil }
func (noopTmux) SelectPane(string) error                  { return nil }
func (noopTmux) SetStatusLine(string) error               { return nil }
func (noopTmux) SetEnv(string, string) error              { return nil }
func (noopTmux) UnsetEnv(string) error                    { return nil }
func (noopTmux) PaneAlive(string) (bool, error)           { return true, nil }
func (noopTmux) RespawnPane(string, string) error         { return nil }
func (noopTmux) CapturePane(string, int) (string, error)  { return "", nil }
func (noopTmux) DetachClient() error                      { return nil }
func (noopTmux) KillSession() error                       { return nil }
func (noopTmux) DisplayPopup(string, string, string) error { return nil }

type noopPersist struct{}

func (noopPersist) Save([]SessionSnapshot) error          { return nil }
func (noopPersist) Load() ([]SessionSnapshot, error)      { return nil, nil }

type noopEventLog struct{}

func (noopEventLog) Append(state.SessionID, string) error { return nil }
func (noopEventLog) Close(state.SessionID)                {}
func (noopEventLog) CloseAll()                            {}

type noopWatcher struct {
	ch chan FSEvent
}

func (n noopWatcher) Watch(state.SessionID, string) error   { return nil }
func (n noopWatcher) Unwatch(state.SessionID) error          { return nil }
func (n noopWatcher) Events() <-chan FSEvent {
	if n.ch == nil {
		return nil
	}
	return n.ch
}
func (n noopWatcher) Close() error { return nil }

// === eventTypeName for diagnostic logging (avoid pulling in fmt %T) ===

func eventTypeName(ev state.Event) string {
	switch ev.(type) {
	case state.EvTick:
		return "EvTick"
	case state.EvCmdCreateSession:
		return "EvCmdCreateSession"
	case state.EvCmdStopSession:
		return "EvCmdStopSession"
	case state.EvCmdHook:
		return "EvCmdHook"
	case state.EvJobResult:
		return "EvJobResult"
	case state.EvTmuxWindowSpawned:
		return "EvTmuxWindowSpawned"
	case state.EvTmuxSpawnFailed:
		return "EvTmuxSpawnFailed"
	case state.EvTranscriptChanged:
		return "EvTranscriptChanged"
	default:
		return "Event"
	}
}

// Compile-time interface assertions.
var (
	_ state.DriverState = driver.GenericState{}
)

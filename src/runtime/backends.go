package runtime

import (
	"github.com/takezoh/agent-roost/state"
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
	// window index (e.g. "1") and the pane id (e.g. "%5").
	SpawnWindow(name, command, startDir string, env map[string]string) (windowIndex, paneID string, err error)

	// KillPaneWindow destroys the tmux window containing the named pane.
	KillPaneWindow(paneTarget string) error

	// RunChain executes a sequence of swap-pane (or other) commands as
	// a single tmux invocation. Used for the swap-pane preview chain.
	RunChain(ops ...[]string) error

	// SwapPane exchanges two pane positions without changing pane ids.
	SwapPane(srcPane, dstPane string) error

	// BreakPane moves a pane into another window.
	BreakPane(srcPane, dstWindow string) error

	// BreakPaneToNewWindow moves a pane into a newly created window and
	// returns that window's index.
	BreakPaneToNewWindow(srcPane, name string) (string, error)

	// JoinPane moves a pane into another pane slot. sizePct controls
	// the new pane size; before inserts before the target pane.
	JoinPane(srcPane, dstPane string, before bool, sizePct int) error

	// PaneID returns the pane id (e.g. "%5") for the target pane.
	PaneID(target string) (string, error)
	// PaneSize returns the visible size of the target pane.
	PaneSize(target string) (width, height int, err error)

	// SelectPane focuses a tmux pane.
	SelectPane(target string) error
	// ResizeWindow resizes the tmux window containing the target.
	ResizeWindow(target string, width, height int) error

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

	// CapturePane returns the trailing nLines of a pane's content (no SGR).
	// Used by polling drivers via the worker pool.
	CapturePane(paneTarget string, nLines int) (string, error)

	// CapturePaneEscaped returns the trailing nLines with ANSI escape sequences
	// preserved (-e flag). Used by the VT-parser-based state detection.
	CapturePaneEscaped(paneTarget string, nLines int) (string, error)

	// InspectPane snapshots a pane's visible state for diagnostics.
	InspectPane(target string, nLines int) (PaneSnapshot, error)

	// ShowEnvironment returns the tmux session environment as a
	// newline-delimited KEY=VALUE string (output of show-environment).
	ShowEnvironment() (string, error)

	// DetachClient detaches the current tmux client.
	DetachClient() error

	// KillSession destroys the roost tmux session.
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
// bag (opaque map of strings). Pane ids are tracked in tmux session env
// vars (ROOST_SESSION_<sid>); sessions.json stays pane-id free.
type SessionSnapshot struct {
	ID        string                 `json:"id"`
	Project   string                 `json:"project"`
	CreatedAt string                 `json:"created_at"`
	Frames    []SessionFrameSnapshot `json:"frames"`
}

type SessionFrameSnapshot struct {
	ID            string              `json:"id"`
	Project       string              `json:"project"`
	Command       string              `json:"command"`
	LaunchOptions state.LaunchOptions `json:"launch_options,omitempty"`
	CreatedAt     string              `json:"created_at"`
	Driver        string              `json:"driver"`
	DriverState   map[string]string   `json:"driver_state"`
}

// EventLogBackend writes per-session event log lines. The
// implementation lazily opens the file on first append and keeps it
// open until Close(sessionID) is called.
type EventLogBackend interface {
	Append(frameID state.FrameID, line string) error
	Close(frameID state.FrameID)
	CloseAll()
}

// ToolLogBackend writes per-project tool-use JSONL lines. Namespace
// identifies the driver (opaque to the runtime). Project is the
// projectDir() slug (e.g. "-workspace-agent-roost"). Files are kept open
// and flushed lazily; CloseAll must be called on shutdown.
type ToolLogBackend interface {
	Append(namespace, project, line string) error
	CloseAll()
}

// FSWatcher is the fsnotify wrapper. It watches per-session
// files and emits FSEvent values on Events() when they change.
type FSWatcher interface {
	Watch(frameID state.FrameID, path string) error
	Unwatch(frameID state.FrameID) error
	Events() <-chan FSEvent
	Close() error
}

// FSEvent is the runtime-side representation of a file change.
// SessionID is set by the watcher (which knows the path → session
// mapping it was given via Watch).
type FSEvent struct {
	FrameID state.FrameID
	Path    string
}

// === noop backends (used until production wiring lands) ===

type noopTmux struct{}

type PaneSnapshot struct {
	Target         string
	Dead           bool
	InMode         bool
	CurrentCommand string
	CursorX        string
	CursorY        string
	ContentTail    string
}

func (noopTmux) SpawnWindow(name, command, startDir string, env map[string]string) (string, string, error) {
	return "", "", nil
}
func (noopTmux) KillPaneWindow(string) error { return nil }
func (noopTmux) RunChain(...[]string) error  { return nil }
func (noopTmux) SwapPane(string, string) error  { return nil }
func (noopTmux) BreakPane(string, string) error { return nil }
func (noopTmux) BreakPaneToNewWindow(string, string) (string, error) {
	return "", nil
}
func (noopTmux) JoinPane(string, string, bool, int) error { return nil }
func (noopTmux) PaneID(string) (string, error)            { return "", nil }
func (noopTmux) PaneSize(string) (int, int, error)        { return 0, 0, nil }
func (noopTmux) SelectPane(string) error                  { return nil }
func (noopTmux) ResizeWindow(string, int, int) error      { return nil }
func (noopTmux) SetStatusLine(string) error               { return nil }
func (noopTmux) SetEnv(string, string) error              { return nil }
func (noopTmux) UnsetEnv(string) error                    { return nil }
func (noopTmux) PaneAlive(string) (bool, error)           { return true, nil }
func (noopTmux) RespawnPane(string, string) error         { return nil }
func (noopTmux) CapturePane(string, int) (string, error)         { return "", nil }
func (noopTmux) CapturePaneEscaped(string, int) (string, error)  { return "", nil }
func (noopTmux) InspectPane(string, int) (PaneSnapshot, error) {
	return PaneSnapshot{}, nil
}
func (noopTmux) ShowEnvironment() (string, error)          { return "", nil }
func (noopTmux) DetachClient() error                       { return nil }
func (noopTmux) KillSession() error                        { return nil }
func (noopTmux) DisplayPopup(string, string, string) error { return nil }

type noopPersist struct{}

func (noopPersist) Save([]SessionSnapshot) error     { return nil }
func (noopPersist) Load() ([]SessionSnapshot, error) { return nil, nil }

type noopEventLog struct{}

func (noopEventLog) Append(state.FrameID, string) error { return nil }
func (noopEventLog) Close(state.FrameID)                {}
func (noopEventLog) CloseAll()                          {}

type noopToolLog struct{}

func (noopToolLog) Append(string, string, string) error { return nil }
func (noopToolLog) CloseAll()                           {}

type noopWatcher struct {
	ch chan FSEvent
}

func (n noopWatcher) Watch(state.FrameID, string) error { return nil }
func (n noopWatcher) Unwatch(state.FrameID) error       { return nil }
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
	case state.EvEvent:
		return "EvEvent"
	case state.EvJobResult:
		return "EvJobResult"
	case state.EvTmuxPaneSpawned:
		return "EvTmuxPaneSpawned"
	case state.EvTmuxSpawnFailed:
		return "EvTmuxSpawnFailed"
	case state.EvFileChanged:
		return "EvFileChanged"
	default:
		return "Event"
	}
}

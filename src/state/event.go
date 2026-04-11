package state

import (
	"encoding/json"
	"time"
)

// Event is the closed sum type of every input the reducer accepts.
// Adding a new event = adding a struct + a Reduce case. The compiler
// + the panic in Reduce's default branch ensures we cover them all.
type Event interface {
	isEvent()
}

// Event type constants for dispatch by reduceEvent.
const (
	EventCreateSession  = "create-session"
	EventStopSession    = "stop-session"
	EventListSessions   = "list-sessions"
	EventPreviewSession = "preview-session"
	EventSwitchSession  = "switch-session"
	EventPreviewProject = "preview-project"
	EventFocusPane      = "focus-pane"
	EventLaunchTool     = "launch-tool"
	EventShutdown       = "shutdown"
	EventDetach         = "detach"
)

// === IPC commands (caller → daemon) ===

// EvCmdSubscribe registers ConnID as a broadcast subscriber. Filters
// is the set of event names to receive; an empty list means all.
type EvCmdSubscribe struct {
	ConnID  ConnID
	ReqID   string
	Filters []string
}

// EvCmdUnsubscribe removes ConnID from the broadcast list without
// closing the connection.
type EvCmdUnsubscribe struct {
	ConnID ConnID
	ReqID  string
}

// EvEvent is a registered command event (create-session, stop-session, etc.)
// dispatched from TUI/tools/keybindings via the registry.
type EvEvent struct {
	ConnID  ConnID
	ReqID   string
	Event   string
	Payload json.RawMessage
}

// EvDriverEvent is a driver hook event from the agent process via
// `roost event <eventType>`. Routed to the session's driver.
type EvDriverEvent struct {
	ConnID    ConnID
	ReqID     string
	Event     string
	Timestamp time.Time
	SenderID  SessionID
	Payload   json.RawMessage
}

// === Connection lifecycle ===

type EvConnOpened struct {
	ConnID ConnID
}

type EvConnClosed struct {
	ConnID ConnID
}

// === Timer / I/O feedback ===

// EvTick is the periodic tick fired by runtime's ticker. Drivers run
// their Step{DEvTick} on every tick. WindowTargets maps each SessionID
// to its tmux window target (index-based, e.g. "1", "2"), pre-filled
// by the runtime so reducers can forward it to drivers without touching
// the windowMap directly.
type EvTick struct {
	Now           time.Time
	WindowTargets map[SessionID]string
}

// EvFileChanged is fired by runtime's fsnotify watcher when a
// session's watched file changes on disk.
type EvFileChanged struct {
	SessionID SessionID
	Path      string
}

// EvJobResult delivers a worker pool job's result back to the reducer.
type EvJobResult struct {
	JobID  JobID
	Result any
	Err    error
}

// EvPaneDied is fired when the runtime detects via tmux display-message
// that a pane is dead. For control panes (0.1 / 0.2) the reducer
// respawns them. For pane 0.0 (active agent), the reducer evicts the
// owning session. OwnerSessionID is set by the runtime (currently
// the active session) when it detects pane 0.0 is dead.
type EvPaneDied struct {
	Pane           string
	OwnerSessionID SessionID // set for pane 0.0 dead detection
}

// EvTmuxWindowVanished is fired by ReconcileWindows when a session
// window has disappeared (agent process exited).
type EvTmuxWindowVanished struct {
	SessionID SessionID
}

// EvTmuxWindowSpawned is the async result of a tmux new-window call
// initiated by EffSpawnTmuxWindow. WindowTarget is the window index
// (e.g. "1", "2") the runtime uses to route activate/kill effects.
type EvTmuxWindowSpawned struct {
	SessionID    SessionID
	WindowTarget string
	ReplyConn    ConnID
	ReplyReqID   string
}

// EvTmuxSpawnFailed is the async failure of a tmux new-window call.
// The reducer evicts the half-created session and replies to the
// original caller with an error.
type EvTmuxSpawnFailed struct {
	SessionID  SessionID
	Err        string
	ReplyConn  ConnID
	ReplyReqID string
}

// === isEvent markers ===

func (EvCmdSubscribe) isEvent()       {}
func (EvCmdUnsubscribe) isEvent()     {}
func (EvEvent) isEvent()              {}
func (EvDriverEvent) isEvent()        {}
func (EvConnOpened) isEvent()         {}
func (EvConnClosed) isEvent()         {}
func (EvTick) isEvent()               {}
func (EvFileChanged) isEvent()        {}
func (EvJobResult) isEvent()          {}
func (EvPaneDied) isEvent()           {}
func (EvTmuxWindowVanished) isEvent() {}
func (EvTmuxWindowSpawned) isEvent()  {}
func (EvTmuxSpawnFailed) isEvent()    {}

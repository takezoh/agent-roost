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
	EventCreateSession    = "create-session"
	EventStopSession      = "stop-session"
	EventListSessions     = "list-sessions"
	EventPreviewSession   = "preview-session"
	EventSwitchSession    = "switch-session"
	EventPreviewProject   = "preview-project"
	EventFocusPane        = "focus-pane"
	EventLaunchTool       = "launch-tool"
	EventShutdown         = "shutdown"
	EventDetach           = "detach"
	EventPushDriver       = "push-driver"
	EventActivateFrame    = "activate-frame"
	EventActivateOccupant = "activate-occupant"
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

// EvCmdSurfaceReadText requests the trailing lines of a session's pane.
type EvCmdSurfaceReadText struct {
	ConnID    ConnID
	ReqID     string
	SessionID SessionID
	Lines     int // 0 = server default
}

// EvCmdSurfaceSendText sends Text + Enter to a session's active pane.
type EvCmdSurfaceSendText struct {
	ConnID    ConnID
	ReqID     string
	SessionID SessionID
	Text      string
}

// EvCmdSurfaceSendKey sends a named key to a session's active pane.
type EvCmdSurfaceSendKey struct {
	ConnID    ConnID
	ReqID     string
	SessionID SessionID
	Key       string
}

// EvCmdDriverList requests the list of registered drivers.
type EvCmdDriverList struct {
	ConnID ConnID
	ReqID  string
}

// EvDriverEvent is a driver hook event from the agent process via
// `roost event <eventType>`. Routed to the session's driver.
type EvDriverEvent struct {
	ConnID    ConnID
	ReqID     string
	Event     string
	Timestamp time.Time
	SenderID  FrameID
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
// their Step{DEvTick} on every tick. PaneTargets maps each SessionID
// to its tmux pane id (e.g. "%5"), pre-filled by the runtime so reducers
// can forward it to drivers without touching the runtime directly.
// N is a monotonic counter used for effect bucketing (gate expensive
// effects to every N-th tick rather than every tick).
type EvTick struct {
	Now         time.Time
	PaneTargets map[SessionID]string
	N           uint64
}

// EvFileChanged is fired by runtime's fsnotify watcher when a
// session's watched file changes on disk.
type EvFileChanged struct {
	FrameID FrameID
	Path    string
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
// owning session. OwnerSessionID is set by the runtime when it detects
// pane 0.0 is dead.
type EvPaneDied struct {
	Pane         string
	OwnerFrameID FrameID // set for pane 0.0 dead detection
}

// EvTmuxWindowVanished is fired by ReconcileWindows when a session
// window has disappeared (agent process exited).
type EvTmuxWindowVanished struct {
	FrameID FrameID
}

// EvTmuxPaneSpawned is the async result of a tmux new-window call
// initiated by EffSpawnTmuxWindow. PaneTarget is the pane id the runtime
// uses to route activate/capture effects.
type EvTmuxPaneSpawned struct {
	SessionID  SessionID
	FrameID    FrameID
	PaneTarget string
	ReplyConn  ConnID
	ReplyReqID string
}

// EvTmuxSpawnFailed is the async failure of a tmux new-window call.
// The reducer evicts the half-created session and replies to the
// original caller with an error.
type EvTmuxSpawnFailed struct {
	SessionID  SessionID
	FrameID    FrameID
	Err        string
	ReplyConn  ConnID
	ReplyReqID string
}

// EvPaneActivity is fired by the PaneTap reader goroutine when bytes arrive
// from a pane's raw stream. The runtime pre-fills PaneTarget and Now so the
// reducer can pass them to the driver without accessing runtime internals.
type EvPaneActivity struct {
	FrameID    FrameID
	PaneTarget string
	Now        time.Time
}

// EvPaneOsc is fired by the PaneTap reader goroutine when an OSC
// notification is detected in the raw byte stream from a pane.
// Title and Body are already parsed from the raw payload.
type EvPaneOsc struct {
	FrameID FrameID
	Cmd     int
	Title   string
	Body    string
	Now     time.Time
}

// === isEvent markers ===

func (EvCmdSubscribe) isEvent()       {}
func (EvCmdUnsubscribe) isEvent()     {}
func (EvCmdSurfaceReadText) isEvent() {}
func (EvCmdSurfaceSendText) isEvent() {}
func (EvCmdSurfaceSendKey) isEvent()  {}
func (EvCmdDriverList) isEvent()      {}
func (EvEvent) isEvent()              {}
func (EvDriverEvent) isEvent()        {}
func (EvConnOpened) isEvent()         {}
func (EvConnClosed) isEvent()         {}
func (EvTick) isEvent()               {}
func (EvFileChanged) isEvent()        {}
func (EvJobResult) isEvent()          {}
func (EvPaneDied) isEvent()           {}
func (EvTmuxWindowVanished) isEvent() {}
func (EvTmuxPaneSpawned) isEvent()    {}
func (EvTmuxSpawnFailed) isEvent()    {}
func (EvPaneActivity) isEvent()       {}
func (EvPaneOsc) isEvent()            {}

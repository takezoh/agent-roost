package state

import "time"

// Event is the closed sum type of every input the reducer accepts.
// Adding a new event = adding a struct + a Reduce case. The compiler
// + the panic in Reduce's default branch ensures we cover them all.
type Event interface {
	isEvent()
}

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

type EvCmdCreateSession struct {
	ConnID  ConnID
	ReqID   string
	Project string
	Command string
}

type EvCmdStopSession struct {
	ConnID    ConnID
	ReqID     string
	SessionID SessionID
}

type EvCmdListSessions struct {
	ConnID ConnID
	ReqID  string
}

type EvCmdPreviewSession struct {
	ConnID    ConnID
	ReqID     string
	SessionID SessionID
}

type EvCmdSwitchSession struct {
	ConnID    ConnID
	ReqID     string
	SessionID SessionID
}

type EvCmdPreviewProject struct {
	ConnID  ConnID
	ReqID   string
	Project string
}

type EvCmdFocusPane struct {
	ConnID ConnID
	ReqID  string
	Pane   string
}

type EvCmdLaunchTool struct {
	ConnID ConnID
	ReqID  string
	Tool   string
	Args   map[string]string
}

// EvCmdHook delivers a typed hook payload from a driver-specific bridge
// (e.g. `roost claude event`). Driver identifies the registered driver
// name; Event is the driver-defined event kind; Payload is the parsed
// hook payload.
type EvCmdHook struct {
	ConnID    ConnID
	ReqID     string
	Driver    string
	Event     string
	SessionID SessionID
	Payload   map[string]any
}

type EvCmdShutdown struct {
	ConnID ConnID
	ReqID  string
}

type EvCmdDetach struct {
	ConnID ConnID
	ReqID  string
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
// their Step{DEvTick} on every tick.
type EvTick struct {
	Now time.Time
}

// EvTranscriptChanged is fired by runtime's fsnotify watcher when a
// session's transcript file changes on disk.
type EvTranscriptChanged struct {
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
// that one of the control panes (0.1 / 0.2) is dead and needs respawn.
type EvPaneDied struct {
	Pane string
}

// EvTmuxWindowVanished is fired by ReconcileWindows when a session
// window has disappeared (agent process exited).
type EvTmuxWindowVanished struct {
	WindowID WindowID
}

// EvTmuxWindowSpawned is the async result of a tmux new-window call
// initiated by EffSpawnTmuxWindow. It carries the freshly assigned
// window id and agent pane id back to the reducer, plus the original
// caller's reply context so the reducer can finish the create-session
// round trip.
type EvTmuxWindowSpawned struct {
	SessionID   SessionID
	WindowID    WindowID
	AgentPaneID string
	ReplyConn   ConnID
	ReplyReqID  string
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
func (EvCmdCreateSession) isEvent()   {}
func (EvCmdStopSession) isEvent()     {}
func (EvCmdListSessions) isEvent()    {}
func (EvCmdPreviewSession) isEvent()  {}
func (EvCmdSwitchSession) isEvent()   {}
func (EvCmdPreviewProject) isEvent()  {}
func (EvCmdFocusPane) isEvent()       {}
func (EvCmdLaunchTool) isEvent()      {}
func (EvCmdHook) isEvent()            {}
func (EvCmdShutdown) isEvent()        {}
func (EvCmdDetach) isEvent()          {}
func (EvConnOpened) isEvent()         {}
func (EvConnClosed) isEvent()         {}
func (EvTick) isEvent()               {}
func (EvTranscriptChanged) isEvent()  {}
func (EvJobResult) isEvent()          {}
func (EvPaneDied) isEvent()           {}
func (EvTmuxWindowVanished) isEvent() {}
func (EvTmuxWindowSpawned) isEvent()  {}
func (EvTmuxSpawnFailed) isEvent()    {}

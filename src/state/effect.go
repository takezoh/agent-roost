package state

// Effect is the closed sum type of every side effect the reducer can
// request. The runtime's effect interpreter (runtime.execute) is the
// only place that turns these into actual I/O. Adding a new effect =
// adding a struct + an interpret case.
type Effect interface {
	isEffect()
}

// === tmux operations (synchronous, fast — interpret inline) ===

// EffSpawnTmuxWindow asks the runtime to create a new tmux window for
// the given session. The runtime executes this and feeds back
// EvTmuxPaneSpawned / EvTmuxSpawnFailed, forwarding the Reply*
// fields so the reducer can complete the create-session round trip.
type EffSpawnTmuxWindow struct {
	SessionID  SessionID
	Project    string
	Command    string
	StartDir   string
	Env        map[string]string
	ReplyConn  ConnID
	ReplyReqID string
}

// EffKillSessionWindow destroys the tmux window containing the given session pane.
// The runtime looks up the pane target from its sessionPanes map.
type EffKillSessionWindow struct {
	SessionID SessionID
}

// EffTerminateSession asks the runtime to send a normal termination
// signal to the session's pane.
type EffTerminateSession struct {
	SessionID SessionID
}

// EffActivateSession moves a session's agent pane into pane 0.0.
// The runtime resolves the current pane target from its sessionPanes map.
type EffActivateSession struct {
	SessionID SessionID
	Reason    string
}

// EffDeactivateSession moves the currently active session back to its
// own window, leaving pane 0.0 showing the main TUI.
type EffDeactivateSession struct{}

// EffRegisterPane records the pane target for a session in the runtime
// and saves it as a tmux session-level env var.
type EffRegisterPane struct {
	SessionID  SessionID
	PaneTarget string
}

// EffUnregisterPane removes a session from the runtime's pane map and
// deletes the corresponding tmux session-level env var.
type EffUnregisterPane struct {
	SessionID SessionID
}

// EffSelectPane focuses a tmux pane.
type EffSelectPane struct {
	Target string
}

// EffSyncStatusLine pushes a string into tmux status-left.
type EffSyncStatusLine struct {
	Line string
}

// EffSetTmuxEnv writes a tmux session-level environment variable.
// Empty Value is treated as unset.
type EffSetTmuxEnv struct {
	Key   string
	Value string
}

// EffUnsetTmuxEnv removes a tmux session-level env var.
type EffUnsetTmuxEnv struct {
	Key string
}

// EffCheckPaneAlive asks the runtime to query #{pane_dead} for the
// named pane. If dead, runtime emits EvPaneDied.
type EffCheckPaneAlive struct {
	Pane string
}

// EffRespawnPane respawns a tmux pane (used by health monitor).
type EffRespawnPane struct {
	Pane string
	Cmd  string
}

// EffDetachClient asks tmux to detach the current client.
type EffDetachClient struct{}

// EffDisplayPopup launches a tmux display-popup for a named tool.
// Tool and Args are structured values — the runtime builds the
// shell command string with proper escaping, avoiding injection.
type EffDisplayPopup struct {
	Width  string
	Height string
	Tool   string
	Args   map[string]string
}

// EffKillSession destroys the roost tmux session.
type EffKillSession struct{}

// === IPC operations ===

// EffSendResponse sends a typed response to a specific connection.
// The Body is encoded by the runtime as a proto.Response value.
type EffSendResponse struct {
	ConnID ConnID
	ReqID  string
	Body   any // proto.Response (kept any here so state pkg doesn't import proto)
}

// EffSendResponseSync writes and flushes a typed response to a specific
// connection before the runtime proceeds to later effects.
type EffSendResponseSync struct {
	ConnID ConnID
	ReqID  string
	Body   any // proto.Response (kept any here so state pkg doesn't import proto)
}

// EffSendError sends an error response. Code is a proto.ErrCode (string)
// kept generic at the state layer.
type EffSendError struct {
	ConnID  ConnID
	ReqID   string
	Code    string
	Message string
	Details map[string]any
}

// EffBroadcastSessionsChanged tells the runtime to build the current
// sessions-changed payload from State and broadcast it to subscribers.
// No payload is carried — runtime reads State directly so we don't
// pay for an extra clone.
type EffBroadcastSessionsChanged struct {
	IsPreview bool
}

// EffBroadcastEvent broadcasts a generic typed event to subscribers
// matching FilterTag (empty = no filter).
type EffBroadcastEvent struct {
	Name      string
	Payload   any
	FilterTag string
}

// EffCloseConn closes a specific connection.
type EffCloseConn struct {
	ConnID ConnID
}

// === Persistence / fs ===

// EffPersistSnapshot tells the runtime to write the current State to
// sessions.json. No payload — runtime reads State directly.
type EffPersistSnapshot struct{}

// EffWatchFile registers a file with the fsnotify watcher.
type EffWatchFile struct {
	SessionID SessionID
	Path      string
	Kind      string
}

// EffUnwatchFile removes a file from the watcher.
type EffUnwatchFile struct {
	SessionID SessionID
}

// EffEventLogAppend appends a single line to a session's event log
// file. The runtime owns the file handles (lazy-opened, kept open
// across appends, closed on session destroy).
type EffEventLogAppend struct {
	SessionID SessionID
	Line      string
}

// EffRemoveManagedWorktree removes a roost-managed git worktree path.
type EffRemoveManagedWorktree struct {
	Path string
}

// === Reconciliation ===

// EffReconcileWindows asks the runtime to compare the live tmux
// window list against state.Sessions and emit EvTmuxWindowVanished
// for any session whose window has disappeared.
type EffReconcileWindows struct{}

// === Async work ===

// JobInput is implemented by all job input types. JobKind returns the
// registry key used to look up the runner.
type JobInput interface {
	JobKind() string
}

// EffStartJob enqueues a job on the worker pool. JobID is allocated
// by the reducer (via State.NextJobID) and recorded in State.Jobs so
// the EvJobResult callback can be routed back to the right session.
type EffStartJob struct {
	JobID JobID
	Input JobInput
}

// === isEffect markers ===

func (EffSpawnTmuxWindow) isEffect()          {}
func (EffKillSessionWindow) isEffect()        {}
func (EffTerminateSession) isEffect()         {}
func (EffActivateSession) isEffect()          {}
func (EffDeactivateSession) isEffect()        {}
func (EffRegisterPane) isEffect()             {}
func (EffUnregisterPane) isEffect()           {}
func (EffSelectPane) isEffect()               {}
func (EffSyncStatusLine) isEffect()           {}
func (EffSetTmuxEnv) isEffect()               {}
func (EffUnsetTmuxEnv) isEffect()             {}
func (EffCheckPaneAlive) isEffect()           {}
func (EffRespawnPane) isEffect()              {}
func (EffDetachClient) isEffect()             {}
func (EffDisplayPopup) isEffect()             {}
func (EffKillSession) isEffect()              {}
func (EffSendResponse) isEffect()             {}
func (EffSendResponseSync) isEffect()         {}
func (EffSendError) isEffect()                {}
func (EffBroadcastSessionsChanged) isEffect() {}
func (EffBroadcastEvent) isEffect()           {}
func (EffCloseConn) isEffect()                {}
func (EffPersistSnapshot) isEffect()          {}
func (EffWatchFile) isEffect()                {}
func (EffUnwatchFile) isEffect()              {}
func (EffEventLogAppend) isEffect()           {}
func (EffRemoveManagedWorktree) isEffect()    {}
func (EffReconcileWindows) isEffect()         {}
func (EffStartJob) isEffect()                 {}

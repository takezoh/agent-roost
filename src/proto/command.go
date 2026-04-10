package proto

// Command is the closed sum type of every IPC request the daemon
// accepts. Each impl carries the typed args + a Name() string that
// matches the wire "cmd" field.
type Command interface {
	isCommand()
	CommandName() string
}

// Command name constants — used by both Encode and Decode so a typo
// breaks both ends symmetrically.
const (
	CmdNameSubscribe       = "subscribe"
	CmdNameUnsubscribe     = "unsubscribe"
	CmdNameCreateSession   = "create-session"
	CmdNameStopSession     = "stop-session"
	CmdNameListSessions    = "list-sessions"
	CmdNamePreviewSession  = "preview-session"
	CmdNameSwitchSession   = "switch-session"
	CmdNamePreviewProject  = "preview-project"
	CmdNameFocusPane       = "focus-pane"
	CmdNameLaunchTool      = "launch-tool"
	CmdNameHook            = "hook"
	CmdNameShutdown        = "shutdown"
	CmdNameDetach          = "detach"
)

type CmdSubscribe struct {
	Filters []string `json:"filters,omitempty"`
}

func (CmdSubscribe) isCommand()           {}
func (CmdSubscribe) CommandName() string  { return CmdNameSubscribe }

type CmdUnsubscribe struct{}

func (CmdUnsubscribe) isCommand()          {}
func (CmdUnsubscribe) CommandName() string { return CmdNameUnsubscribe }

type CmdCreateSession struct {
	Project string `json:"project"`
	Command string `json:"command,omitempty"`
}

func (CmdCreateSession) isCommand()          {}
func (CmdCreateSession) CommandName() string { return CmdNameCreateSession }

type CmdStopSession struct {
	SessionID string `json:"session_id"`
}

func (CmdStopSession) isCommand()          {}
func (CmdStopSession) CommandName() string { return CmdNameStopSession }

type CmdListSessions struct{}

func (CmdListSessions) isCommand()          {}
func (CmdListSessions) CommandName() string { return CmdNameListSessions }

type CmdPreviewSession struct {
	SessionID string `json:"session_id"`
}

func (CmdPreviewSession) isCommand()          {}
func (CmdPreviewSession) CommandName() string { return CmdNamePreviewSession }

type CmdSwitchSession struct {
	SessionID string `json:"session_id"`
}

func (CmdSwitchSession) isCommand()          {}
func (CmdSwitchSession) CommandName() string { return CmdNameSwitchSession }

type CmdPreviewProject struct {
	Project string `json:"project"`
}

func (CmdPreviewProject) isCommand()          {}
func (CmdPreviewProject) CommandName() string { return CmdNamePreviewProject }

type CmdFocusPane struct {
	Pane string `json:"pane"`
}

func (CmdFocusPane) isCommand()          {}
func (CmdFocusPane) CommandName() string { return CmdNameFocusPane }

type CmdLaunchTool struct {
	Tool string            `json:"tool"`
	Args map[string]string `json:"args,omitempty"`
}

func (CmdLaunchTool) isCommand()          {}
func (CmdLaunchTool) CommandName() string { return CmdNameLaunchTool }

// CmdHook is the wire form of a driver-specific hook event delivered
// from the hook bridge (`roost claude event`). Driver names the
// registered driver, Event is the driver-defined event kind, Payload
// is the parsed hook body. The reducer translates this into the
// typed driver Step input.
type CmdHook struct {
	Driver    string         `json:"driver"`
	Event     string         `json:"event"`
	SessionID string         `json:"session_id"`
	Payload   map[string]any `json:"payload,omitempty"`
}

func (CmdHook) isCommand()          {}
func (CmdHook) CommandName() string { return CmdNameHook }

type CmdShutdown struct{}

func (CmdShutdown) isCommand()          {}
func (CmdShutdown) CommandName() string { return CmdNameShutdown }

type CmdDetach struct{}

func (CmdDetach) isCommand()          {}
func (CmdDetach) CommandName() string { return CmdNameDetach }

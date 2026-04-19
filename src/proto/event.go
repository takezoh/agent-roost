package proto

// ServerEvent is the closed sum type of broadcasts the daemon pushes
// to subscribed clients. Each impl carries the typed payload + a
// Name() string that matches the wire "name" field.
type ServerEvent interface {
	isEvent()
	EventName() string
}

const (
	EvtNameSessionsChanged   = "sessions-changed"
	EvtNameProjectSelected   = "project-selected"
	EvtNamePaneFocused       = "pane-focused"
	EvtNameLogLine           = "log-line"
	EvtNameSessionFileLine   = "session-file-line"
	EvtNameAgentNotification = "agent-notification"
	EvtNamePeerMessage       = "peer-message"
)

// EvtSessionsChanged carries the current session table. Sent on
// every state change that affects what the TUI should render.
type EvtSessionsChanged struct {
	Sessions        []SessionInfo   `json:"sessions"`
	ActiveSessionID string          `json:"active_session_id,omitempty"`
	IsPreview       bool            `json:"is_preview,omitempty"`
	Connectors      []ConnectorInfo `json:"connectors,omitempty"`
	Features        []string        `json:"features,omitempty"`
}

func (EvtSessionsChanged) isEvent()          {}
func (EvtSessionsChanged) EventName() string { return EvtNameSessionsChanged }

// EvtProjectSelected fires when the user picks a project from the
// session list (preview-project IPC).
type EvtProjectSelected struct {
	Project string `json:"project"`
}

func (EvtProjectSelected) isEvent()          {}
func (EvtProjectSelected) EventName() string { return EvtNameProjectSelected }

// EvtPaneFocused fires after focus-pane changes the active control
// pane.
type EvtPaneFocused struct {
	Pane string `json:"pane"`
}

func (EvtPaneFocused) isEvent()          {}
func (EvtPaneFocused) EventName() string { return EvtNamePaneFocused }

// EvtLogLine pushes one new line of the global daemon log to TUI
// subscribers (Phase 7).
type EvtLogLine struct {
	Path string `json:"path"`
	Line string `json:"line"`
}

func (EvtLogLine) isEvent()          {}
func (EvtLogLine) EventName() string { return EvtNameLogLine }

// EvtSessionFileLine pushes one new line from a session's log/transcript
// file to TUI subscribers.
type EvtSessionFileLine struct {
	SessionID string `json:"session_id"`
	Kind      string `json:"kind"`
	Line      string `json:"line"`
}

func (EvtSessionFileLine) isEvent()          {}
func (EvtSessionFileLine) EventName() string { return EvtNameSessionFileLine }

// EvtAgentNotification is emitted when an OSC 9/99/777 notification
// escape is captured from an agent pane.
type EvtAgentNotification struct {
	SessionID string `json:"session_id"`
	Cmd       int    `json:"cmd"`
	Title     string `json:"title,omitempty"`
	Body      string `json:"body,omitempty"`
}

func (EvtAgentNotification) isEvent()          {}
func (EvtAgentNotification) EventName() string { return EvtNameAgentNotification }

// EvtPeerMessage is pushed to TUI subscribers when a peer message is
// injected or queued.
type EvtPeerMessage struct {
	ToSessionID string `json:"to_session_id"`
	FromFrameID string `json:"from_frame_id"`
	Text        string `json:"text"`
	SentAt      string `json:"sent_at"` // RFC3339
}

func (EvtPeerMessage) isEvent()          {}
func (EvtPeerMessage) EventName() string { return EvtNamePeerMessage }

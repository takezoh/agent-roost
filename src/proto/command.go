package proto

import (
	"encoding/json"
	"time"
)

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
	CmdNameSubscribe   = "subscribe"
	CmdNameUnsubscribe = "unsubscribe"
	CmdNameEvent       = "event"

	// surface.* — pane read/write operations
	CmdNameSurfaceReadText = "surface.read_text"
	CmdNameSurfaceSendText = "surface.send_text"
	CmdNameSurfaceSendKey  = "surface.send_key"

	// driver.* — driver registry queries
	CmdNameDriverList = "driver.list"
)


type CmdSubscribe struct {
	Filters []string `json:"filters,omitempty"`
}

func (CmdSubscribe) isCommand()          {}
func (CmdSubscribe) CommandName() string { return CmdNameSubscribe }

type CmdUnsubscribe struct{}

func (CmdUnsubscribe) isCommand()          {}
func (CmdUnsubscribe) CommandName() string { return CmdNameUnsubscribe }

// CmdEvent is the generic event envelope sent by the `roost event` CLI.
type CmdEvent struct {
	Event     string          `json:"event"`
	Timestamp time.Time       `json:"timestamp"`
	SenderID  string          `json:"sender_id"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

func (CmdEvent) isCommand()          {}
func (CmdEvent) CommandName() string { return CmdNameEvent }

// CmdSurfaceReadText reads the trailing Lines of a session's pane content.
// SessionID identifies the target session; Lines=0 uses the server default (30).
type CmdSurfaceReadText struct {
	SessionID string `json:"session_id"`
	Lines     int    `json:"lines,omitempty"`
}

func (CmdSurfaceReadText) isCommand()          {}
func (CmdSurfaceReadText) CommandName() string { return CmdNameSurfaceReadText }

// CmdSurfaceSendText sends Text followed by Enter to a session's active pane.
type CmdSurfaceSendText struct {
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
}

func (CmdSurfaceSendText) isCommand()          {}
func (CmdSurfaceSendText) CommandName() string { return CmdNameSurfaceSendText }

// CmdSurfaceSendKey sends a named key (e.g. "Escape", "C-c") to a session's
// active pane without appending Enter.
type CmdSurfaceSendKey struct {
	SessionID string `json:"session_id"`
	Key       string `json:"key"`
}

func (CmdSurfaceSendKey) isCommand()          {}
func (CmdSurfaceSendKey) CommandName() string { return CmdNameSurfaceSendKey }

// CmdDriverList lists all registered driver names and display names.
type CmdDriverList struct{}

func (CmdDriverList) isCommand()          {}
func (CmdDriverList) CommandName() string { return CmdNameDriverList }

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

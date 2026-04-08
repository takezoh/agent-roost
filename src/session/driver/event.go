package driver

import "strings"

// AgentEventType identifies the lifecycle hook an AgentEvent represents.
type AgentEventType string

const (
	AgentEventSessionStart AgentEventType = "session-start"
	AgentEventStateChange  AgentEventType = "state-change"
)

// AgentEvent is the driver-neutral runtime payload an agent driver subcommand
// (e.g. `roost claude event`) sends to the coordinator after parsing its
// tool-specific hook format.
//
// All driver-specific data lives inside DriverState — a map[string]string
// whose keys are defined by the driver. The core layer never reads these keys;
// it merges them as-is into Session.DriverState and lets driver methods
// interpret them. Adding a new driver-specific field requires no changes
// outside the driver implementation and the driver's hook bridge.
type AgentEvent struct {
	Type        AgentEventType
	Pane        string // tmux pane that fired the event (if known)
	State       string // state-change payload: "running" / "waiting" / ...
	Log         string // human-readable line for the event log file
	DriverState map[string]string
}

// driverStateArgPrefix tags map keys belonging to DriverState in the wire
// format. core's IPC currently uses a flat map[string]string, so we namespace
// driver keys to avoid colliding with the generic fields.
const driverStateArgPrefix = "drv_"

// ToArgs serializes an AgentEvent into the wire-level map[string]string the
// existing IPC client uses. Empty fields are omitted so the wire stays compact
// and FromArgs can distinguish "not provided" from "explicit empty".
func (e AgentEvent) ToArgs() map[string]string {
	args := map[string]string{
		"type": string(e.Type),
	}
	if e.State != "" {
		args["state"] = e.State
	}
	if e.Pane != "" {
		args["pane"] = e.Pane
	}
	if e.Log != "" {
		args["log"] = e.Log
	}
	for k, v := range e.DriverState {
		if v == "" {
			continue
		}
		args[driverStateArgPrefix+k] = v
	}
	return args
}

// AgentEventFromArgs parses the wire-level args map produced by ToArgs back
// into a typed AgentEvent. This is the single place in core that touches
// raw IPC keys; the rest of core sees only struct fields.
func AgentEventFromArgs(args map[string]string) AgentEvent {
	ev := AgentEvent{
		Type:  AgentEventType(args["type"]),
		State: args["state"],
		Pane:  args["pane"],
		Log:   args["log"],
	}
	for k, v := range args {
		if strings.HasPrefix(k, driverStateArgPrefix) {
			if ev.DriverState == nil {
				ev.DriverState = make(map[string]string)
			}
			ev.DriverState[strings.TrimPrefix(k, driverStateArgPrefix)] = v
		}
	}
	return ev
}

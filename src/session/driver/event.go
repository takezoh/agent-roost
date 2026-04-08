package driver

// AgentEventType identifies the lifecycle hook an AgentEvent represents.
type AgentEventType string

const (
	AgentEventSessionStart AgentEventType = "session-start"
	AgentEventStateChange  AgentEventType = "state-change"
)

// AgentEvent is the driver-neutral runtime payload an agent driver subcommand
// (e.g. `roost claude event`) sends to the coordinator after parsing its
// tool-specific hook format. core treats every driver the same way and never
// reaches into the original Claude/Gemini/... payload directly — Claude hook
// fields like `cwd` and `transcript_path` are translated into AgentEvent
// fields exactly once, in lib/claude/command.go.
type AgentEvent struct {
	Type           AgentEventType
	AgentSessionID string // agent's own session identifier
	WorkingDir     string // absolute working directory the agent process runs in
	TranscriptPath string // absolute path of the agent's transcript file
	State          string // state-change payload: "running" / "waiting" / ...
	Pane           string // tmux pane that fired the event (if known)
	Log            string // human-readable line for the event log file
}

// ToArgs serializes an AgentEvent into the wire-level map[string]string the
// existing IPC client uses. Empty fields are omitted so the wire stays compact
// and FromArgs can distinguish "not provided" from "explicit empty".
func (e AgentEvent) ToArgs() map[string]string {
	args := map[string]string{
		"type":       string(e.Type),
		"session_id": e.AgentSessionID,
	}
	if e.WorkingDir != "" {
		args["working_dir"] = e.WorkingDir
	}
	if e.TranscriptPath != "" {
		args["transcript_path"] = e.TranscriptPath
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
	return args
}

// AgentEventFromArgs parses the wire-level args map produced by ToArgs back
// into a typed AgentEvent. This is the single place in core that touches
// raw IPC keys; the rest of core sees only struct fields.
func AgentEventFromArgs(args map[string]string) AgentEvent {
	return AgentEvent{
		Type:           AgentEventType(args["type"]),
		AgentSessionID: args["session_id"],
		WorkingDir:     args["working_dir"],
		TranscriptPath: args["transcript_path"],
		State:          args["state"],
		Pane:           args["pane"],
		Log:            args["log"],
	}
}

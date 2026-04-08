package driver

import "fmt"

type AgentState int

const (
	AgentStateUnset   AgentState = -1
	AgentStateIdle    AgentState = 0
	AgentStateRunning AgentState = 1
	AgentStateWaiting AgentState = 2
	AgentStatePending AgentState = 3
	AgentStateStopped AgentState = 4
)

var agentStateNames = map[AgentState]string{
	AgentStateUnset:   "unset",
	AgentStateIdle:    "idle",
	AgentStateRunning: "running",
	AgentStateWaiting: "waiting",
	AgentStatePending: "pending",
	AgentStateStopped: "stopped",
}

func (s AgentState) String() string {
	if name, ok := agentStateNames[s]; ok {
		return name
	}
	return "unknown"
}

type AgentSession struct {
	ID         string
	State      AgentState
	StatusLine string
	Title      string
	LastPrompt string
	Subjects   []string

	// Driver-derived insight fields. These are populated by ResolveMeta
	// and consumed via Indicators() to keep the core layer agnostic of
	// any single driver's concepts.
	AgentName      string
	CurrentTool    string
	RecentCommands []string
	SubagentCounts map[string]int
	ErrorCount     int
	TouchedFiles   []string
}

// Indicators returns the driver-formatted status chips shown next to the
// session card in the sessions view. Returning nil hides the chip line.
func (a *AgentSession) Indicators() []string {
	var out []string
	if a.CurrentTool != "" {
		out = append(out, "▸ "+a.CurrentTool)
	}
	subs := 0
	for _, n := range a.SubagentCounts {
		subs += n
	}
	if subs > 0 {
		out = append(out, fmt.Sprintf("%d subs", subs))
	}
	if a.ErrorCount > 0 {
		out = append(out, fmt.Sprintf("%d err", a.ErrorCount))
	}
	return out
}

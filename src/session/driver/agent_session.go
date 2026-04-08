package driver

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
}

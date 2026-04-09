package driver

import "fmt"

// AgentSession holds metadata an agent driver reports about its session,
// independently of the dynamic operational status (running / waiting / etc.)
// which lives in state.Store. AgentStore caches one AgentSession per agent
// session ID; status is intentionally absent here so the two layers stay
// decoupled.
type AgentSession struct {
	ID         string
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

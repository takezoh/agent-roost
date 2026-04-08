package core

// SessionTracker incrementally consumes a transcript file for an agent
// session and produces a tmux status-line string. Concrete drivers
// (e.g. lib/transcript.Tracker for Claude) live outside the core
// package; main wires them via Service.SetTracker.
type SessionTracker interface {
	Update(agentSessionID, transcriptPath string) (statusLine string, changed bool)
}

// noopTracker is the default tracker installed by NewService. It does
// nothing and is replaced via SetTracker once a real driver is wired.
type noopTracker struct{}

func (noopTracker) Update(_, _ string) (string, bool) { return "", false }

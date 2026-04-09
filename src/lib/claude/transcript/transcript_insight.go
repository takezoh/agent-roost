package transcript

// SessionInsight is the rolling, aggregated view of a transcript that
// callers (status line, sessions view) can render. It is updated
// incrementally by UpdateInsight as new entries arrive.
type SessionInsight struct {
	CurrentTool    string         // most recent tool_use name awaiting a result
	RecentCommands []string       // up to 5 most recent Bash commands (newest last)
	SubagentCounts map[string]int // agentType -> launches
	ErrorCount     int            // tool_result with is_error=true so far
	TouchedFiles   []string       // up to 10 unique file paths from Read/Write/Edit
	AgentName      string         // agent-name event (Claude-assigned slug)
}

// MetaSnapshot bundles the legacy session-meta fields with a freshly
// built SessionInsight. Returned by AggregateMeta.
type MetaSnapshot struct {
	Title      string
	LastPrompt string
	Subjects   []string
	Insight    SessionInsight
}

const (
	maxRecentCommands = 5
	maxTouchedFiles   = 10
	maxSubjectsAgg    = 10
)

// AggregateMeta collapses an entry slice into a MetaSnapshot.
func AggregateMeta(entries []Entry) MetaSnapshot {
	var snap MetaSnapshot
	for _, e := range entries {
		applyEntryToMeta(&snap, e)
	}
	return snap
}

// UpdateInsight folds incoming entries into an existing insight.
func UpdateInsight(insight *SessionInsight, entries []Entry) {
	for _, e := range entries {
		applyEntryToInsight(insight, e)
	}
}

func applyEntryToMeta(snap *MetaSnapshot, e Entry) {
	switch e.Kind {
	case KindCustomTitle:
		snap.Title = e.Text
	case KindLastPrompt:
		snap.LastPrompt = e.Text
	case KindToolUse:
		if e.ToolName == "TaskCreate" && e.ToolInput.Primary != "" && len(snap.Subjects) < maxSubjectsAgg {
			snap.Subjects = append(snap.Subjects, e.ToolInput.Primary)
		}
	}
	applyEntryToInsight(&snap.Insight, e)
}

func applyEntryToInsight(insight *SessionInsight, e Entry) {
	switch e.Kind {
	case KindAgentName:
		insight.AgentName = e.Text
	case KindToolUse:
		insight.CurrentTool = e.ToolName
		switch e.ToolName {
		case "Bash":
			if e.ToolInput.Primary != "" {
				insight.RecentCommands = appendBoundedUnique(insight.RecentCommands, e.ToolInput.Primary, maxRecentCommands)
			}
		case "Read", "Write", "Edit", "MultiEdit":
			if e.ToolInput.Primary != "" {
				insight.TouchedFiles = appendBoundedUnique(insight.TouchedFiles, e.ToolInput.Primary, maxTouchedFiles)
			}
		}
	case KindToolResult:
		insight.CurrentTool = ""
		if e.IsError {
			insight.ErrorCount++
		}
		if e.ToolName == "Task" || e.ToolName == "Agent" {
			if ar, ok := e.ToolResult.(AgentResult); ok && ar.AgentType != "" {
				if insight.SubagentCounts == nil {
					insight.SubagentCounts = map[string]int{}
				}
				insight.SubagentCounts[ar.AgentType]++
			}
		}
	}
}

// appendBoundedUnique pushes v onto the end of list, avoiding duplicates
// (existing entries are moved to the end) and trimming the list to max
// items by dropping the oldest.
func appendBoundedUnique(list []string, v string, max int) []string {
	for i, s := range list {
		if s == v {
			list = append(list[:i], list[i+1:]...)
			break
		}
	}
	list = append(list, v)
	if len(list) > max {
		list = list[len(list)-max:]
	}
	return list
}

// SubagentTotal sums the SubagentCounts map.
func (i SessionInsight) SubagentTotal() int {
	n := 0
	for _, v := range i.SubagentCounts {
		n += v
	}
	return n
}

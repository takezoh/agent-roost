package driver

import (
	"path/filepath"

	"github.com/takezoh/agent-roost/state"
)

// CommonTags returns the shared UI tags (e.g. Git branch) for the driver.
func CommonTags(c CommonState) []state.Tag {
	var tags []state.Tag
	if t := BranchTag(c.BranchTag, c.BranchBG, c.BranchFG, c.BranchParentBranch); t.Text != "" {
		tags = append(tags, t)
	}
	return tags
}

// EventLogTab returns the "EVENTS" log tab if the session and log directory are known.
func EventLogTab(c CommonState, eventLogDir string) *state.LogTab {
	if c.RoostSessionID != "" && eventLogDir != "" {
		return &state.LogTab{
			Label: "EVENTS",
			Path:  filepath.Join(eventLogDir, c.RoostSessionID+".log"),
			Kind:  state.TabKindText,
		}
	}
	return nil
}

// firstNonEmpty returns the first string in candidates that is not empty.
func firstNonEmpty(candidates ...string) string {
	for _, s := range candidates {
		if s != "" {
			return s
		}
	}
	return ""
}

// previewText truncates long text for display in info lines.
func previewText(text string) string {
	const max = 80
	// Simple truncation; could be improved with ellipsis if needed.
	if len(text) > max {
		return text[:max] + "..."
	}
	return text
}

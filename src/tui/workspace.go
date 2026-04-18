package tui

import (
	"sort"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/proto"
)

// workspaceChipHitbox records the half-open x range a workspace chip occupies
// on the workspace bar row, used for mouse hit-testing.
type workspaceChipHitbox struct {
	name string
	x0   int
	x1   int
}

// workspaceBarLayout renders the workspace switcher chip row and returns the
// hitboxes for each chip. It is a pure function so View() and mouse
// hit-testing both call it and always get consistent results.
func workspaceBarLayout(names []string, selected string) (string, []workspaceChipHitbox) {
	var parts []string
	boxes := make([]workspaceChipHitbox, 0, len(names))
	x := 0

	for _, name := range names {
		var rendered string
		if selected == name {
			rendered = workspaceChipOnStyle.Render(name)
		} else {
			rendered = workspaceChipOffStyle.Render(name)
		}
		w := lipgloss.Width(rendered)
		boxes = append(boxes, workspaceChipHitbox{name: name, x0: x, x1: x + w})
		parts = append(parts, rendered)
		x += w + 1
	}

	return strings.Join(parts, " "), boxes
}

// workspaceBarVisible reports whether the workspace switcher bar should be
// rendered. The bar is omitted when only one workspace exists (or none),
// since there is nothing meaningful to switch between.
func (m Model) workspaceBarVisible() bool {
	return len(m.workspaces) >= 2
}

// hitTestWorkspaceChip maps a mouse coordinate to a workspace chip. The
// workspace bar is the second header row (row 1, just below the title).
// Returns hit=false when the bar is not visible or the click is not on any chip.
func (m Model) hitTestWorkspaceChip(x, y int) (name string, hit bool) {
	if !m.workspaceBarVisible() {
		return "", false
	}
	const workspaceRow = 1
	if y != workspaceRow {
		return "", false
	}
	_, boxes := workspaceBarLayout(m.workspaces, m.selectedWorkspace)
	for _, b := range boxes {
		if x >= b.x0 && x < b.x1 {
			return b.name, true
		}
	}
	return "", false
}

// collectWorkspaces returns the sorted list of distinct workspace names found
// across the given sessions. "default" is always included as the first entry.
func collectWorkspaces(sessions []proto.SessionInfo) []string {
	seen := make(map[string]struct{})
	seen[config.DefaultWorkspaceName] = struct{}{}
	for _, s := range sessions {
		ws := workspaceOf(&s)
		seen[ws] = struct{}{}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		// "default" always comes first.
		if names[i] == config.DefaultWorkspaceName {
			return true
		}
		if names[j] == config.DefaultWorkspaceName {
			return false
		}
		return names[i] < names[j]
	})
	return names
}

// workspaceOf returns the workspace name for a session, falling back to
// DefaultWorkspaceName when the field is empty (e.g. from an old daemon).
func workspaceOf(s *proto.SessionInfo) string {
	if s.Workspace == "" {
		return config.DefaultWorkspaceName
	}
	return s.Workspace
}

// nextWorkspace cycles forward through names, wrapping from last to first.
func nextWorkspace(names []string, current string) string {
	if len(names) == 0 {
		return current
	}
	for i, n := range names {
		if n == current {
			return names[(i+1)%len(names)]
		}
	}
	return names[0]
}

// prevWorkspace cycles backward through names, wrapping from first to last.
func prevWorkspace(names []string, current string) string {
	if len(names) == 0 {
		return current
	}
	for i, n := range names {
		if n == current {
			return names[(i-1+len(names))%len(names)]
		}
	}
	return names[0]
}

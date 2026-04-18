package tui

import (
	"testing"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/state"
)

func TestRebuildItems_FiltersByWorkspace(t *testing.T) {
	m := Model{
		folded: make(map[string]bool),
		filter: allOnFilter(),
		sessions: []proto.SessionInfo{
			{ID: "s1", Project: "/tmp/work-proj", Workspace: "work",
				View: state.View{DisplayName: "work-proj"}},
			{ID: "s2", Project: "/tmp/oss-proj", Workspace: "oss",
				View: state.View{DisplayName: "oss-proj"}},
			{ID: "s3", Project: "/tmp/default-proj", Workspace: "",
				View: state.View{DisplayName: "default-proj"}},
		},
		selectedWorkspace: "work",
	}

	m.rebuildItems()

	// Only the "work" session should appear in items (+ project header).
	sessionCount := 0
	for _, it := range m.items {
		if !it.isProject {
			sessionCount++
			if it.session.Workspace != "work" {
				t.Errorf("unexpected session workspace %q in filtered list", it.session.Workspace)
			}
		}
	}
	if sessionCount != 1 {
		t.Errorf("visible sessions = %d, want 1", sessionCount)
	}
}

func TestRebuildItems_DefaultWorkspaceFilters(t *testing.T) {
	m := Model{
		folded: make(map[string]bool),
		filter: allOnFilter(),
		sessions: []proto.SessionInfo{
			{ID: "s1", Project: "/tmp/work-proj", Workspace: "work",
				View: state.View{DisplayName: "work-proj"}},
			{ID: "s2", Project: "/tmp/default-proj", Workspace: "",
				View: state.View{DisplayName: "default-proj"}},
		},
		selectedWorkspace: config.DefaultWorkspaceName,
	}

	m.rebuildItems()

	// Only the session with no workspace (→ default) should appear.
	sessionCount := 0
	for _, it := range m.items {
		if !it.isProject {
			sessionCount++
		}
	}
	if sessionCount != 1 {
		t.Errorf("visible sessions = %d, want 1 (default workspace only)", sessionCount)
	}
}

func TestRebuildItems_ResetsSelectedWorkspaceWhenMissing(t *testing.T) {
	m := Model{
		folded: make(map[string]bool),
		filter: allOnFilter(),
		sessions: []proto.SessionInfo{
			{ID: "s1", Project: "/tmp/proj", Workspace: "oss",
				View: state.View{DisplayName: "proj"}},
		},
		selectedWorkspace: "work", // "work" no longer exists
	}

	m.rebuildItems()

	if m.selectedWorkspace != config.DefaultWorkspaceName {
		t.Errorf("selectedWorkspace = %q after workspace disappeared, want %q", m.selectedWorkspace, config.DefaultWorkspaceName)
	}
}

func TestRebuildItems_CollectsWorkspaces(t *testing.T) {
	m := Model{
		folded:            make(map[string]bool),
		filter:            allOnFilter(),
		selectedWorkspace: config.DefaultWorkspaceName,
		sessions: []proto.SessionInfo{
			{ID: "s1", Project: "/tmp/w", Workspace: "work",
				View: state.View{DisplayName: "w"}},
			{ID: "s2", Project: "/tmp/o", Workspace: "oss",
				View: state.View{DisplayName: "o"}},
		},
	}

	m.rebuildItems()

	// Workspaces should include "default", "oss", "work".
	if len(m.workspaces) != 3 {
		t.Errorf("workspaces = %v, want [default oss work]", m.workspaces)
	}
	if m.workspaces[0] != "default" {
		t.Errorf("first workspace = %q, want default", m.workspaces[0])
	}
}

package tui

import (
	"testing"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/state"
)

func TestHandleServerEventFollowsWorkspaceOnActiveChange(t *testing.T) {
	m := Model{
		selectedWorkspace: config.DefaultWorkspaceName,
		folded:            make(map[string]bool),
		filter:            allOnFilter(),
		sessions: []proto.SessionInfo{
			{ID: "s1", Project: "/a", Workspace: config.DefaultWorkspaceName, View: state.View{DisplayName: "a"}},
		},
	}
	m.rebuildItems()

	result, _ := m.handleServerEvent(proto.EvtSessionsChanged{
		ActiveSessionID: "s2",
		Sessions: []proto.SessionInfo{
			{ID: "s1", Project: "/a", Workspace: config.DefaultWorkspaceName, View: state.View{DisplayName: "a"}},
			{ID: "s2", Project: "/b", Workspace: "work", View: state.View{DisplayName: "b"}},
		},
	})
	got := result.(Model)
	if got.selectedWorkspace != "work" {
		t.Errorf("selectedWorkspace = %q, want %q", got.selectedWorkspace, "work")
	}
	if got.active != "s2" {
		t.Errorf("active = %q, want %q", got.active, "s2")
	}
	if got.findSessionCursorByID("s2") < 0 {
		t.Error("new session s2 not found in items after workspace follow")
	}
}

func TestHandleServerEventDoesNotFollowWorkspaceOnPreview(t *testing.T) {
	m := Model{
		selectedWorkspace: config.DefaultWorkspaceName,
		folded:            make(map[string]bool),
		filter:            allOnFilter(),
		sessions: []proto.SessionInfo{
			{ID: "s1", Project: "/a", Workspace: config.DefaultWorkspaceName, View: state.View{DisplayName: "a"}},
		},
	}
	m.rebuildItems()

	result, _ := m.handleServerEvent(proto.EvtSessionsChanged{
		ActiveSessionID: "s2",
		IsPreview:       true,
		Sessions: []proto.SessionInfo{
			{ID: "s1", Project: "/a", Workspace: config.DefaultWorkspaceName, View: state.View{DisplayName: "a"}},
			{ID: "s2", Project: "/b", Workspace: "work", View: state.View{DisplayName: "b"}},
		},
	})
	got := result.(Model)
	if got.selectedWorkspace != config.DefaultWorkspaceName {
		t.Errorf("selectedWorkspace = %q, want %q (preview must not switch workspace)", got.selectedWorkspace, config.DefaultWorkspaceName)
	}
}

func TestHandleServerEventKeepsWorkspaceWhenActiveInSameWorkspace(t *testing.T) {
	m := Model{
		active:            "s1",
		selectedWorkspace: config.DefaultWorkspaceName,
		folded:            make(map[string]bool),
		filter:            allOnFilter(),
		sessions: []proto.SessionInfo{
			{ID: "s1", Project: "/a", Workspace: config.DefaultWorkspaceName, View: state.View{DisplayName: "a"}},
			{ID: "s2", Project: "/b", Workspace: config.DefaultWorkspaceName, View: state.View{DisplayName: "b"}},
		},
	}
	m.rebuildItems()

	result, _ := m.handleServerEvent(proto.EvtSessionsChanged{
		ActiveSessionID: "s2",
		Sessions: []proto.SessionInfo{
			{ID: "s1", Project: "/a", Workspace: config.DefaultWorkspaceName, View: state.View{DisplayName: "a"}},
			{ID: "s2", Project: "/b", Workspace: config.DefaultWorkspaceName, View: state.View{DisplayName: "b"}},
		},
	})
	got := result.(Model)
	if got.selectedWorkspace != config.DefaultWorkspaceName {
		t.Errorf("selectedWorkspace = %q, want %q", got.selectedWorkspace, config.DefaultWorkspaceName)
	}
	if got.active != "s2" {
		t.Errorf("active = %q, want %q", got.active, "s2")
	}
}

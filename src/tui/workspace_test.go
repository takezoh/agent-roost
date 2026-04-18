package tui

import (
	"testing"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/proto"
)

// --- collectWorkspaces ---

func TestCollectWorkspaces_AlwaysIncludesDefault(t *testing.T) {
	ws := collectWorkspaces(nil)
	if len(ws) == 0 {
		t.Fatal("expected at least one workspace (default)")
	}
	if ws[0] != config.DefaultWorkspaceName {
		t.Errorf("first workspace = %q, want %q", ws[0], config.DefaultWorkspaceName)
	}
}

func TestCollectWorkspaces_DefaultFirst(t *testing.T) {
	sessions := []proto.SessionInfo{
		{Workspace: "work"},
		{Workspace: "oss"},
		{Workspace: ""},
	}
	ws := collectWorkspaces(sessions)
	if ws[0] != config.DefaultWorkspaceName {
		t.Errorf("first = %q, want %q", ws[0], config.DefaultWorkspaceName)
	}
}

func TestCollectWorkspaces_Sorted(t *testing.T) {
	sessions := []proto.SessionInfo{
		{Workspace: "zzz"},
		{Workspace: "aaa"},
		{Workspace: "mmm"},
	}
	ws := collectWorkspaces(sessions)
	// ws[0] = "default", then aaa, mmm, zzz
	if len(ws) != 4 {
		t.Fatalf("len = %d, want 4", len(ws))
	}
	if ws[1] != "aaa" || ws[2] != "mmm" || ws[3] != "zzz" {
		t.Errorf("order = %v, want [default aaa mmm zzz]", ws)
	}
}

func TestCollectWorkspaces_Deduplicates(t *testing.T) {
	sessions := []proto.SessionInfo{
		{Workspace: "work"},
		{Workspace: "work"},
		{Workspace: "work"},
	}
	ws := collectWorkspaces(sessions)
	if len(ws) != 2 {
		t.Errorf("len = %d, want 2 (default + work)", len(ws))
	}
}

// --- workspaceOf ---

func TestWorkspaceOf_EmptyFallsToDefault(t *testing.T) {
	s := &proto.SessionInfo{}
	if got := workspaceOf(s); got != config.DefaultWorkspaceName {
		t.Errorf("workspaceOf empty = %q, want %q", got, config.DefaultWorkspaceName)
	}
}

func TestWorkspaceOf_ReturnsSet(t *testing.T) {
	s := &proto.SessionInfo{Workspace: "oss"}
	if got := workspaceOf(s); got != "oss" {
		t.Errorf("workspaceOf = %q, want oss", got)
	}
}

// --- nextWorkspace / prevWorkspace ---

func TestNextWorkspace_CyclesWithoutAll(t *testing.T) {
	names := []string{"default", "oss", "work"}
	// default → oss → work → default (wraps)
	if got := nextWorkspace(names, "default"); got != "oss" {
		t.Errorf("next(default) = %q, want oss", got)
	}
	if got := nextWorkspace(names, "oss"); got != "work" {
		t.Errorf("next(oss) = %q, want work", got)
	}
	if got := nextWorkspace(names, "work"); got != "default" {
		t.Errorf("next(work) = %q, want default (wrap)", got)
	}
}

func TestPrevWorkspace_CyclesWithoutAll(t *testing.T) {
	names := []string{"default", "oss", "work"}
	// default → work → oss → default (wraps)
	if got := prevWorkspace(names, "default"); got != "work" {
		t.Errorf("prev(default) = %q, want work (wrap)", got)
	}
	if got := prevWorkspace(names, "work"); got != "oss" {
		t.Errorf("prev(work) = %q, want oss", got)
	}
	if got := prevWorkspace(names, "oss"); got != "default" {
		t.Errorf("prev(oss) = %q, want default", got)
	}
}

func TestNextPrevWorkspace_EmptyNames(t *testing.T) {
	if got := nextWorkspace(nil, "default"); got != "default" {
		t.Errorf("next with no names = %q, want 'default'", got)
	}
	if got := prevWorkspace(nil, "default"); got != "default" {
		t.Errorf("prev with no names = %q, want 'default'", got)
	}
}

func TestNextWorkspace_UnknownCurrentWrapsToFirst(t *testing.T) {
	names := []string{"default", "work"}
	if got := nextWorkspace(names, "nonexistent"); got != "default" {
		t.Errorf("next(unknown) = %q, want 'default'", got)
	}
}

func TestPrevWorkspace_UnknownCurrentWrapsToFirst(t *testing.T) {
	names := []string{"default", "work"}
	if got := prevWorkspace(names, "nonexistent"); got != "default" {
		t.Errorf("prev(unknown) = %q, want 'default'", got)
	}
}

// --- workspaceBarLayout hitboxes ---

func TestWorkspaceBarLayout_HitboxOrder(t *testing.T) {
	names := []string{"default", "work"}
	_, boxes := workspaceBarLayout(names, "default")
	if len(boxes) != 2 { // default, work (no All)
		t.Fatalf("len(boxes) = %d, want 2", len(boxes))
	}
	if boxes[0].name != "default" {
		t.Errorf("boxes[0].name = %q, want default", boxes[0].name)
	}
	if boxes[1].name != "work" {
		t.Errorf("boxes[1].name = %q, want work", boxes[1].name)
	}
}

func TestWorkspaceBarLayout_HitboxesOrdered(t *testing.T) {
	names := []string{"default", "work"}
	_, boxes := workspaceBarLayout(names, "work")
	for i := 1; i < len(boxes); i++ {
		if boxes[i].x0 <= boxes[i-1].x0 {
			t.Errorf("boxes[%d].x0=%d <= boxes[%d].x0=%d; hitboxes should be ordered left-to-right", i, boxes[i].x0, i-1, boxes[i-1].x0)
		}
	}
}

// --- hitTestWorkspaceChip ---

func TestHitTestWorkspaceChip_WrongRowReturnsNoHit(t *testing.T) {
	// Need 2+ workspaces so the bar is visible; then verify row 0 (title) doesn't hit.
	m := Model{workspaces: []string{"default", "work"}}
	_, hit := m.hitTestWorkspaceChip(0, 0) // row 0 = title row, not workspace row
	if hit {
		t.Error("expected no hit on row 0 (title row)")
	}
}

func TestHitTestWorkspaceChip_HiddenWhenSingleWorkspace(t *testing.T) {
	m := Model{workspaces: []string{"default"}}
	_, hit := m.hitTestWorkspaceChip(0, 1)
	if hit {
		t.Error("expected no hit when only one workspace exists (bar hidden)")
	}
}

func TestHitTestWorkspaceChip_LastChipHit(t *testing.T) {
	m := Model{
		workspaces:        []string{"default", "work"},
		selectedWorkspace: "default",
	}
	_, boxes := workspaceBarLayout(m.workspaces, m.selectedWorkspace)
	lastBox := boxes[len(boxes)-1]
	midX := (lastBox.x0 + lastBox.x1) / 2
	name, hit := m.hitTestWorkspaceChip(midX, 1) // row 1 = workspace row
	if !hit {
		t.Fatal("expected hit on last chip")
	}
	if name != "work" {
		t.Errorf("name = %q, want work", name)
	}
}

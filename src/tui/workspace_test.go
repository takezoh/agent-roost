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

func TestNextWorkspace_CyclesThroughAll(t *testing.T) {
	names := []string{"default", "oss", "work"}
	// All → default → oss → work → All
	if got := nextWorkspace(names, ""); got != "default" {
		t.Errorf("next(All) = %q, want default", got)
	}
	if got := nextWorkspace(names, "default"); got != "oss" {
		t.Errorf("next(default) = %q, want oss", got)
	}
	if got := nextWorkspace(names, "oss"); got != "work" {
		t.Errorf("next(oss) = %q, want work", got)
	}
	if got := nextWorkspace(names, "work"); got != "" {
		t.Errorf("next(work) = %q, want '' (All)", got)
	}
}

func TestPrevWorkspace_WrapsFromAll(t *testing.T) {
	names := []string{"default", "oss", "work"}
	// All → work → oss → default → All
	if got := prevWorkspace(names, ""); got != "work" {
		t.Errorf("prev(All) = %q, want work", got)
	}
	if got := prevWorkspace(names, "work"); got != "oss" {
		t.Errorf("prev(work) = %q, want oss", got)
	}
	if got := prevWorkspace(names, "oss"); got != "default" {
		t.Errorf("prev(oss) = %q, want default", got)
	}
	if got := prevWorkspace(names, "default"); got != "" {
		t.Errorf("prev(default) = %q, want '' (All)", got)
	}
}

func TestNextPrevWorkspace_EmptyNames(t *testing.T) {
	if got := nextWorkspace(nil, ""); got != "" {
		t.Errorf("next with no names = %q, want ''", got)
	}
	if got := prevWorkspace(nil, ""); got != "" {
		t.Errorf("prev with no names = %q, want ''", got)
	}
}

func TestNextWorkspace_UnknownCurrentResetsToAll(t *testing.T) {
	names := []string{"default", "work"}
	if got := nextWorkspace(names, "nonexistent"); got != "" {
		t.Errorf("next(unknown) = %q, want ''", got)
	}
}

// --- workspaceBarLayout hitboxes ---

func TestWorkspaceBarLayout_HitboxOrder(t *testing.T) {
	names := []string{"default", "work"}
	_, boxes := workspaceBarLayout(names, "")
	if len(boxes) != 3 { // default, work, All
		t.Fatalf("len(boxes) = %d, want 3", len(boxes))
	}
	if boxes[0].name != "default" || boxes[0].isAll {
		t.Errorf("boxes[0] = {name:%q isAll:%v}, want {default false}", boxes[0].name, boxes[0].isAll)
	}
	if boxes[1].name != "work" || boxes[1].isAll {
		t.Errorf("boxes[1] = {name:%q isAll:%v}, want {work false}", boxes[1].name, boxes[1].isAll)
	}
	if !boxes[2].isAll {
		t.Errorf("boxes[2].isAll = false, want true")
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
	_, _, hit := m.hitTestWorkspaceChip(0, 0) // row 0 = title row, not workspace row
	if hit {
		t.Error("expected no hit on row 0 (title row)")
	}
}

func TestHitTestWorkspaceChip_HiddenWhenSingleWorkspace(t *testing.T) {
	m := Model{workspaces: []string{"default"}}
	_, _, hit := m.hitTestWorkspaceChip(0, 1)
	if hit {
		t.Error("expected no hit when only one workspace exists (bar hidden)")
	}
}

func TestHitTestWorkspaceChip_AllChipHit(t *testing.T) {
	m := Model{
		workspaces:        []string{"default", "work"},
		selectedWorkspace: "default",
	}
	_, boxes := workspaceBarLayout(m.workspaces, m.selectedWorkspace)
	allBox := boxes[len(boxes)-1]
	midX := (allBox.x0 + allBox.x1) / 2
	name, isAll, hit := m.hitTestWorkspaceChip(midX, 1) // row 1 = workspace row
	if !hit {
		t.Fatal("expected hit on All chip")
	}
	if !isAll {
		t.Errorf("isAll = false, want true")
	}
	_ = name
}

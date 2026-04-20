package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/takezoh/agent-roost/tools"
)

func TestPaletteChainsToNextTool(t *testing.T) {
	var ranA, ranB bool
	registry := tools.NewRegistry()
	registry.Register(tools.Tool{
		Name: "tool-a",
		Run: func(ctx *tools.ToolContext, args map[string]string) (*tools.ToolInvocation, error) {
			ranA = true
			return &tools.ToolInvocation{
				Name: "tool-b",
				Args: map[string]string{"y": "from-a"},
			}, nil
		},
	})
	registry.Register(tools.Tool{
		Name: "tool-b",
		Params: []tools.Param{
			{Name: "y", Options: func(ctx *tools.ToolContext) []string { return nil }},
			{Name: "z", Options: func(ctx *tools.ToolContext) []string { return []string{"opt"} }},
		},
		Run: func(ctx *tools.ToolContext, args map[string]string) (*tools.ToolInvocation, error) {
			ranB = true
			return nil, nil
		},
	})

	ctx := &tools.ToolContext{}
	m := NewPaletteModel(registry, ctx, "tool-a")
	model, cmd := m.startTool(registry.Get("tool-a"))

	if !ranA {
		t.Fatal("tool-a Run was not invoked")
	}
	if ranB {
		t.Fatal("tool-b Run should not be invoked yet (still needs param z)")
	}
	if cmd != nil {
		t.Errorf("expected nil cmd while waiting for param input, got non-nil")
	}
	pm, ok := model.(PaletteModel)
	if !ok {
		t.Fatalf("expected PaletteModel, got %T", model)
	}
	if pm.selectedTool == nil || pm.selectedTool.Name != "tool-b" {
		t.Errorf("expected selectedTool = tool-b, got %+v", pm.selectedTool)
	}
	if pm.paramArgs["y"] != "from-a" {
		t.Errorf("expected paramArgs[y]=from-a, got %q", pm.paramArgs["y"])
	}
	if pm.paramIndex != 1 {
		t.Errorf("expected paramIndex=1 (y prefilled, prompting z), got %d", pm.paramIndex)
	}
	if pm.phase != phaseParamSelect {
		t.Errorf("expected phaseParamSelect, got %v", pm.phase)
	}
}

func TestPaletteQuitsWithoutChain(t *testing.T) {
	var ran bool
	registry := tools.NewRegistry()
	registry.Register(tools.Tool{
		Name: "solo",
		Run: func(ctx *tools.ToolContext, args map[string]string) (*tools.ToolInvocation, error) {
			ran = true
			return nil, nil
		},
	})

	ctx := &tools.ToolContext{}
	m := NewPaletteModel(registry, ctx, "solo")
	_, cmd := m.startTool(registry.Get("solo"))

	if !ran {
		t.Fatal("solo Run was not invoked")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit cmd, got nil")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected QuitMsg, got %T", msg)
	}
}

func TestVisibleWindow(t *testing.T) {
	tests := []struct {
		name                      string
		cursor, total, maxVisible int
		wantStart, wantEnd        int
	}{
		{"all fit", 0, 5, 10, 0, 5},
		{"maxVisible zero", 3, 10, 0, 0, 10},
		{"maxVisible negative", 3, 10, -1, 0, 10},
		{"cursor at start", 0, 20, 8, 0, 8},
		{"cursor at end", 19, 20, 8, 12, 20},
		{"cursor in middle", 10, 20, 8, 6, 14},
		{"cursor near start", 2, 20, 8, 0, 8},
		{"cursor near end", 18, 20, 8, 12, 20},
		{"exact fit", 0, 5, 5, 0, 5},
		{"one over", 3, 6, 5, 1, 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := visibleWindow(tt.cursor, tt.total, tt.maxVisible)
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("visibleWindow(%d, %d, %d) = (%d, %d), want (%d, %d)",
					tt.cursor, tt.total, tt.maxVisible, start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestRenderPaletteToolScroll(t *testing.T) {
	registry := tools.NewRegistry()
	for i := 0; i < 30; i++ {
		name := fmt.Sprintf("tool-%02d", i)
		registry.Register(tools.Tool{
			Name:        name,
			Description: "desc",
			Run: func(ctx *tools.ToolContext, args map[string]string) (*tools.ToolInvocation, error) {
				return nil, nil
			},
		})
	}

	m := NewPaletteModel(registry, &tools.ToolContext{}, "")
	m.width = 60
	m.height = 15 // maxVisible = 15 - PanelChromeRows(2) - 2(prompt+blank) = 11; items = 11 - 2(indicators) = 9

	// cursor at 0: no top indicator, bottom indicator
	m.cursor = 0
	out := renderPaletteTool(m, 56)
	if !strings.Contains(out, "▸ tool-00") {
		t.Error("cursor item tool-00 should be visible")
	}
	if strings.Contains(out, "↑") {
		t.Error("should not show top indicator when cursor at 0")
	}
	if !strings.Contains(out, "↓") {
		t.Error("should show bottom indicator")
	}

	// cursor at 15: both indicators
	m.cursor = 15
	out = renderPaletteTool(m, 56)
	if !strings.Contains(out, "▸ tool-15") {
		t.Error("cursor item tool-15 should be visible")
	}
	if !strings.Contains(out, "↑") {
		t.Error("should show top indicator")
	}
	if !strings.Contains(out, "↓") {
		t.Error("should show bottom indicator")
	}

	// cursor at 29 (last): top indicator, no bottom
	m.cursor = 29
	out = renderPaletteTool(m, 56)
	if !strings.Contains(out, "▸ tool-29") {
		t.Error("cursor item tool-29 should be visible")
	}
	if !strings.Contains(out, "↑") {
		t.Error("should show top indicator")
	}
	if strings.Contains(out, "↓") {
		t.Error("should not show bottom indicator when cursor at end")
	}
}

func TestRenderPaletteToolNoScrollWhenFits(t *testing.T) {
	registry := tools.NewRegistry()
	for i := 0; i < 3; i++ {
		registry.Register(tools.Tool{
			Name: fmt.Sprintf("t%d", i),
			Run: func(ctx *tools.ToolContext, args map[string]string) (*tools.ToolInvocation, error) {
				return nil, nil
			},
		})
	}

	m := NewPaletteModel(registry, &tools.ToolContext{}, "")
	m.width = 60
	m.height = 20
	out := renderPaletteTool(m, 56)
	if strings.Contains(out, "↑") || strings.Contains(out, "↓") {
		t.Error("should not show scroll indicators when all items fit")
	}
}

func TestPaletteViewDoesNotPanicAfterToolExecution(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(tools.Tool{
		Name: "two-param",
		Params: []tools.Param{
			{Name: "a", Options: func(ctx *tools.ToolContext) []string { return []string{"x"} }},
			{Name: "b", Options: func(ctx *tools.ToolContext) []string { return []string{"y"} }},
		},
		Run: func(ctx *tools.ToolContext, args map[string]string) (*tools.ToolInvocation, error) {
			return nil, nil
		},
	})

	m := NewPaletteModel(registry, &tools.ToolContext{}, "two-param")
	m.width = 60
	m.height = 20
	// Simulate state after all params filled and tool executed:
	// paramIndex == len(Params), phase still phaseParamSelect
	m.phase = phaseParamSelect
	m.selectedTool = registry.Get("two-param")
	m.paramIndex = len(m.selectedTool.Params)

	// Must not panic
	_ = m.View()
}

func TestRenderPaletteParamWorktreeChipOnCursorOnly(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(tools.Tool{
		Name: "new-session",
		Params: []tools.Param{
			{Name: "command", Options: func(ctx *tools.ToolContext) []string {
				return []string{"cmd-a", "cmd-b", "cmd-c"}
			}},
		},
		Run: func(ctx *tools.ToolContext, args map[string]string) (*tools.ToolInvocation, error) {
			return nil, nil
		},
	})

	m := NewPaletteModel(registry, &tools.ToolContext{}, "")
	m.width = 60
	m.height = 20
	m.phase = phaseParamSelect
	m.selectedTool = registry.Get("new-session")
	m.paramIndex = 0
	m.paramOptions = []string{"cmd-a", "cmd-b", "cmd-c"}
	m.worktreeOn = true
	m.projectIsGit = true
	m.paramCursor = 1

	out := renderPaletteParam(m, 56)

	// cursor row (cmd-b) must contain the chip
	lines := strings.Split(out, "\n")
	cursorLine := ""
	for _, l := range lines {
		if strings.Contains(l, "cmd-b") {
			cursorLine = l
		}
	}
	if !strings.Contains(cursorLine, "wt on") {
		t.Errorf("cursor line should contain 'wt on', got: %q", cursorLine)
	}

	// non-cursor rows must not contain the chip
	for _, l := range lines {
		if strings.Contains(l, "cmd-a") || strings.Contains(l, "cmd-c") {
			if strings.Contains(l, "wt ") {
				t.Errorf("non-cursor line should not contain worktree chip, got: %q", l)
			}
		}
	}

	// when worktreeOn=false the chip shows "wt off"
	m.worktreeOn = false
	out2 := renderPaletteParam(m, 56)
	for _, l := range strings.Split(out2, "\n") {
		if strings.Contains(l, "cmd-b") && strings.Contains(l, "wt off") {
			return
		}
	}
	t.Error("cursor line should contain 'wt off' when worktreeOn is false")
}

func TestRenderPaletteParamNoChipForOtherTools(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(tools.Tool{
		Name: "other-tool",
		Params: []tools.Param{
			{Name: "command", Options: func(ctx *tools.ToolContext) []string {
				return []string{"opt-a", "opt-b"}
			}},
		},
		Run: func(ctx *tools.ToolContext, args map[string]string) (*tools.ToolInvocation, error) {
			return nil, nil
		},
	})

	m := NewPaletteModel(registry, &tools.ToolContext{}, "")
	m.width = 60
	m.height = 20
	m.phase = phaseParamSelect
	m.selectedTool = registry.Get("other-tool")
	m.paramIndex = 0
	m.paramOptions = []string{"opt-a", "opt-b"}
	m.worktreeOn = true
	m.paramCursor = 0

	out := renderPaletteParam(m, 56)
	if strings.Contains(out, "wt ") {
		t.Error("worktree chip should not appear for non-new-session tools")
	}
}

func TestRenderPaletteParamNoChipForNonGitProject(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(tools.Tool{
		Name: "new-session",
		Params: []tools.Param{
			{Name: "command", Options: func(ctx *tools.ToolContext) []string {
				return []string{"opt-a", "opt-b"}
			}},
		},
		Run: func(ctx *tools.ToolContext, args map[string]string) (*tools.ToolInvocation, error) {
			return nil, nil
		},
	})

	m := NewPaletteModel(registry, &tools.ToolContext{}, "")
	m.width = 60
	m.height = 20
	m.phase = phaseParamSelect
	m.selectedTool = registry.Get("new-session")
	m.paramIndex = 0
	m.paramOptions = []string{"opt-a", "opt-b"}
	m.worktreeOn = true // pre-set to on; should be suppressed by projectIsGit=false
	m.projectIsGit = false
	m.paramCursor = 0

	out := renderPaletteParam(m, 56)
	if strings.Contains(out, "wt ") {
		t.Error("worktree chip should not appear when projectIsGit is false")
	}
}

func TestAdvanceParamDisablesWorktreeForNonGitProject(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(tools.Tool{
		Name: "new-session",
		Params: []tools.Param{
			{Name: "project", Options: func(ctx *tools.ToolContext) []string { return nil }},
			{Name: "command", Options: func(ctx *tools.ToolContext) []string { return []string{"sh"} }},
		},
		Run: func(ctx *tools.ToolContext, args map[string]string) (*tools.ToolInvocation, error) {
			return nil, nil
		},
	})

	nonGitDir := t.TempDir() // plain dir, not a git repo
	ctx := &tools.ToolContext{
		Args: map[string]string{
			"project":  nonGitDir,
			"worktree": "on", // stale pre-fill
		},
		IsGitProject: func(path string) bool { return false },
	}

	m := NewPaletteModel(registry, ctx, "new-session")
	m.worktreeOn = true // simulate pre-fill via ctx.Args
	model, _ := m.startTool(registry.Get("new-session"))
	pm := model.(PaletteModel)

	if pm.worktreeOn {
		t.Error("worktreeOn should be false for non-git project")
	}
	if _, ok := pm.paramArgs["worktree"]; ok {
		t.Error("paramArgs[worktree] should be cleared for non-git project")
	}
	if pm.projectIsGit {
		t.Error("projectIsGit should be false for non-git project")
	}
}

func TestFilterParamOptionsMultiToken(t *testing.T) {
	registry := tools.NewRegistry()
	ctx := &tools.ToolContext{}
	m := NewPaletteModel(registry, ctx, "")
	m.paramOptions = []string{"/workspace/agent-roost", "/home/x/other-project"}

	// single token in basename
	m.input = "agent"
	got := m.filterParamOptions()
	if len(got) != 1 || got[0].Value != "/workspace/agent-roost" {
		t.Errorf("input='agent': got %v, want [/workspace/agent-roost]", got)
	}

	// two tokens: one matches basename, one matches dir
	m.input = "roost work"
	got = m.filterParamOptions()
	if len(got) != 1 || got[0].Value != "/workspace/agent-roost" {
		t.Errorf("input='roost work': got %v, want [/workspace/agent-roost]", got)
	}
	if len(got[0].DisplayIndexes) == 0 {
		t.Error("input='roost work': DisplayIndexes should be non-empty")
	}
	if len(got[0].SuffixIndexes) == 0 {
		t.Error("input='roost work': SuffixIndexes should be non-empty (work matches /workspace dir)")
	}

	// space-separated tokens that are both in basename
	m.input = "agent roost"
	got = m.filterParamOptions()
	if len(got) != 1 || got[0].Value != "/workspace/agent-roost" {
		t.Errorf("input='agent roost': got %v, want [/workspace/agent-roost]", got)
	}

	// one token matches nothing → zero results
	m.input = "agent zzz"
	got = m.filterParamOptions()
	if len(got) != 0 {
		t.Errorf("input='agent zzz': got %v, want empty", got)
	}

	// all-whitespace → all options, no indexes
	m.input = "   "
	got = m.filterParamOptions()
	if len(got) != 2 {
		t.Errorf("input='   ': len = %d, want 2", len(got))
	}
	for _, mo := range got {
		if len(mo.DisplayIndexes) != 0 || len(mo.SuffixIndexes) != 0 {
			t.Error("all-whitespace input should produce no indexes")
		}
	}
}

func TestPaletteIgnoresUnknownChainTarget(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(tools.Tool{
		Name: "tool-a",
		Run: func(ctx *tools.ToolContext, args map[string]string) (*tools.ToolInvocation, error) {
			return &tools.ToolInvocation{Name: "missing"}, nil
		},
	})

	ctx := &tools.ToolContext{}
	m := NewPaletteModel(registry, ctx, "tool-a")
	_, cmd := m.startTool(registry.Get("tool-a"))

	if cmd == nil {
		t.Fatal("expected tea.Quit cmd when chained tool is not found")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("expected QuitMsg fallback, got %T", cmd())
	}
}

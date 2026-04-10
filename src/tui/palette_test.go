package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/take/agent-roost/tools"
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

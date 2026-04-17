package tools

import (
	"testing"
)

func TestHiddenToolExcludedFromAll(t *testing.T) {
	r := NewRegistry()
	r.Register(Tool{Name: "visible", Description: "visible"})
	r.Register(Tool{Name: "hidden", Description: "hidden", Hidden: true})

	all := r.All()
	for _, t2 := range all {
		if t2.Name == "hidden" {
			t.Error("hidden tool should not appear in All()")
		}
	}
	if len(all) != 1 {
		t.Errorf("All() len = %d, want 1", len(all))
	}
}

func TestHiddenToolExcludedFromMatch(t *testing.T) {
	r := NewRegistry()
	r.Register(Tool{Name: "visible", Description: "visible"})
	r.Register(Tool{Name: "hidden", Description: "hidden", Hidden: true})

	matched := r.Match("")
	for _, t2 := range matched {
		if t2.Tool.Name == "hidden" {
			t.Error("hidden tool should not appear in Match()")
		}
	}
	matched2 := r.Match("hidden")
	if len(matched2) != 0 {
		t.Errorf("Match('hidden') = %v, want empty", matched2)
	}
}

func TestGetReturnsHiddenTool(t *testing.T) {
	r := NewRegistry()
	r.Register(Tool{Name: "hidden", Description: "hidden", Hidden: true})

	got := r.Get("hidden")
	if got == nil {
		t.Fatal("Get(hidden) should return the tool")
	}
	if got.Name != "hidden" {
		t.Errorf("Get(hidden).Name = %q, want hidden", got.Name)
	}
}

func TestPushDriverToolIsHidden(t *testing.T) {
	r := DefaultRegistry()
	got := r.Get("push-driver")
	if got == nil {
		t.Fatal("push-driver not registered")
	}
	if !got.Hidden {
		t.Error("push-driver should be Hidden")
	}
	// Should not appear in All().
	for _, tool := range r.All() {
		if tool.Name == "push-driver" {
			t.Error("push-driver should not appear in All()")
		}
	}
}

func TestMatchEmptyQueryReturnsAll(t *testing.T) {
	r := NewRegistry()
	r.Register(Tool{Name: "alpha"})
	r.Register(Tool{Name: "beta"})
	r.Register(Tool{Name: "hidden-tool", Hidden: true})

	got := r.Match("")
	if len(got) != 2 {
		t.Fatalf("Match('') len = %d, want 2", len(got))
	}
	if got[0].Tool.Name != "alpha" || got[1].Tool.Name != "beta" {
		t.Errorf("Match('') order = %v/%v, want alpha/beta", got[0].Tool.Name, got[1].Tool.Name)
	}
	for _, m := range got {
		if len(m.Indexes) != 0 {
			t.Error("empty query should produce no match indexes")
		}
	}
}

func TestMatchFuzzySubsequence(t *testing.T) {
	r := NewRegistry()
	r.Register(Tool{Name: "new-session"})
	r.Register(Tool{Name: "stop-session"})
	r.Register(Tool{Name: "detach"})

	got := r.Match("sess")
	names := make([]string, len(got))
	for i, m := range got {
		names[i] = m.Tool.Name
	}
	// Both session tools match "sess" as subsequence; detach does not
	if len(got) != 2 {
		t.Fatalf("Match('sess') = %v, want 2 results", names)
	}
	for _, m := range got {
		if len(m.Indexes) == 0 {
			t.Errorf("Match('sess') %q: expected non-empty indexes", m.Tool.Name)
		}
	}
}

func TestMatchNoResults(t *testing.T) {
	r := NewRegistry()
	r.Register(Tool{Name: "new-session"})

	got := r.Match("zzz")
	if len(got) != 0 {
		t.Errorf("Match('zzz') = %v, want empty", got)
	}
}

func TestPushDriverToolRequiresSessionID(t *testing.T) {
	r := DefaultRegistry()
	tool := r.Get("push-driver")
	if tool == nil {
		t.Fatal("push-driver not registered")
	}
	ctx := &ToolContext{
		Client: nil, // won't be reached if session_id validation fires first
		Args:   map[string]string{},
	}
	_, err := tool.Run(ctx, map[string]string{"command": "shell"})
	if err == nil {
		t.Fatal("expected error when session_id is empty")
	}
}

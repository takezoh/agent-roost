package tools

import "testing"

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
		if t2.Name == "hidden" {
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

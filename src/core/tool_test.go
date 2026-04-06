package core

import "testing"

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	r.Register(Tool{Name: "alpha", Description: "desc"})
	if got := r.Get("alpha"); got == nil || got.Name != "alpha" {
		t.Fatal("expected registered tool")
	}
}

func TestRegistryGetMissing(t *testing.T) {
	r := NewRegistry()
	if r.Get("nonexistent") != nil {
		t.Fatal("expected nil for missing tool")
	}
}

func TestRegistryAll(t *testing.T) {
	r := NewRegistry()
	r.Register(Tool{Name: "a"})
	r.Register(Tool{Name: "b"})
	if len(r.All()) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(r.All()))
	}
}

func TestRegistryMatch(t *testing.T) {
	r := NewRegistry()
	r.Register(Tool{Name: "foo-bar"})
	r.Register(Tool{Name: "baz-qux"})
	if len(r.Match("foo")) != 1 {
		t.Fatal("expected 1 match for foo")
	}
	if len(r.Match("")) != 2 {
		t.Fatal("expected all tools for empty query")
	}
}

func TestRegistryMatchByDescription(t *testing.T) {
	r := NewRegistry()
	r.Register(Tool{Name: "x", Description: "hello world"})
	if len(r.Match("hello")) != 1 {
		t.Fatal("expected match by description")
	}
}

func TestDefaultRegistry(t *testing.T) {
	r := DefaultRegistry()
	if len(r.All()) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(r.All()))
	}
	for _, name := range []string{"new-session", "shutdown"} {
		if r.Get(name) == nil {
			t.Fatalf("missing tool %s", name)
		}
	}
}

func TestProjectDisplayName(t *testing.T) {
	if got := ProjectDisplayName("/home/user/my-project"); got != "my-project" {
		t.Fatalf("expected my-project, got %s", got)
	}
}

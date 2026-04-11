package state

import (
	"encoding/json"
	"testing"
)

func TestRegisterTabRendererAndCreate(t *testing.T) {
	kind := TabKind("_test_kind")

	type testCfg struct {
		Foo string `json:"foo"`
	}

	var gotFoo string
	RegisterTabRenderer[testCfg](kind, func(cfg testCfg) TabRenderer {
		gotFoo = cfg.Foo
		return &stubRenderer{}
	})

	raw, _ := json.Marshal(testCfg{Foo: "bar"})
	r := NewTabRenderer(kind, raw)
	if r == nil {
		t.Fatal("expected non-nil renderer")
	}
	if gotFoo != "bar" {
		t.Errorf("config not passed: got %q, want %q", gotFoo, "bar")
	}
}

func TestNewTabRendererUnknownKind(t *testing.T) {
	r := NewTabRenderer(TabKind("_nonexistent"), nil)
	if r != nil {
		t.Error("expected nil for unregistered kind")
	}
}

func TestRegisterTabRendererNilConfig(t *testing.T) {
	kind := TabKind("_test_nil_cfg")

	type testCfg struct {
		Val int `json:"val"`
	}

	var called bool
	RegisterTabRenderer[testCfg](kind, func(cfg testCfg) TabRenderer {
		called = true
		if cfg.Val != 0 {
			t.Errorf("expected zero value, got %d", cfg.Val)
		}
		return &stubRenderer{}
	})

	r := NewTabRenderer(kind, nil)
	if r == nil {
		t.Fatal("expected non-nil renderer")
	}
	if !called {
		t.Error("factory not called")
	}
}

type stubRenderer struct{}

func (stubRenderer) Append([]byte) string { return "" }
func (stubRenderer) Reset()               {}

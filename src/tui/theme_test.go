package tui

import (
	"reflect"
	"testing"
)

func TestApplyTheme_KnownNames(t *testing.T) {
	t.Cleanup(func() { ApplyTheme("default") })

	ApplyTheme("default")
	if Active.Minimal {
		t.Errorf("default theme should have Minimal=false")
	}

	ApplyTheme("minimal")
	if !Active.Minimal {
		t.Errorf("minimal theme should have Minimal=true")
	}
}

func TestApplyTheme_UnknownFallsBackToDefault(t *testing.T) {
	t.Cleanup(func() { ApplyTheme("default") })

	ApplyTheme("nonexistent-theme")
	if Active.Minimal {
		t.Errorf("unknown theme should fall back to default (Minimal=false)")
	}
}

func TestApplyTheme_EmptyNameFallsBackToDefault(t *testing.T) {
	t.Cleanup(func() { ApplyTheme("default") })

	ApplyTheme("")
	if Active.Minimal {
		t.Errorf("empty theme name should fall back to default")
	}
}

func TestThemes_DefaultAndMinimalShareColors(t *testing.T) {
	def := Themes["default"]
	min := Themes["minimal"]

	// Zero out the rendering toggle so equality only compares colors.
	def.Minimal = false
	min.Minimal = false

	if !reflect.DeepEqual(def, min) {
		t.Errorf("default and minimal themes should share identical colors; differ: %+v vs %+v", def, min)
	}
}

func TestApplyTheme_RebuildsStyles(t *testing.T) {
	t.Cleanup(func() { ApplyTheme("default") })

	// After ApplyTheme, the package-level style vars must be initialized
	// (non-zero). Use titleStyle as a representative sample.
	ApplyTheme("default")
	if titleStyle.Render("x") == "" {
		t.Errorf("titleStyle should render non-empty after ApplyTheme")
	}
	if tagStyle.Render("x") == "" {
		t.Errorf("tagStyle should render non-empty after ApplyTheme")
	}
}

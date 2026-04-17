package tui

import (
	"testing"

	"charm.land/lipgloss/v2"
)

func TestBuiltinThemesLoaded(t *testing.T) {
	for _, name := range []string{"default", "minimal"} {
		th, ok := Themes[name]
		if !ok {
			t.Fatalf("builtin theme %q not in registry", name)
		}
		if th.Primary == nil {
			t.Errorf("theme %q: Primary is nil", name)
		}
	}
}

func TestMinimalThemeFlag(t *testing.T) {
	def := Themes["default"]
	min := Themes["minimal"]
	if def.Minimal {
		t.Error("default theme should have Minimal=false")
	}
	if !min.Minimal {
		t.Error("minimal theme should have Minimal=true")
	}
}

func TestRegisterAndApplyCustomTheme(t *testing.T) {
	custom := newBaseTheme()
	custom.Primary = lipgloss.Color("#AABBCC")
	RegisterTheme("test-custom", custom)

	th, ok := Themes["test-custom"]
	if !ok {
		t.Fatal("registered theme not found")
	}
	if th.Primary != lipgloss.Color("#AABBCC") {
		t.Errorf("Primary mismatch: got %v", th.Primary)
	}

	// Applying it should not panic and should set Active.
	ApplyTheme("test-custom")
	if Active.Primary != lipgloss.Color("#AABBCC") {
		t.Errorf("Active.Primary not updated after ApplyTheme")
	}

	// Restore default so other tests are not affected.
	ApplyTheme("default")
	delete(Themes, "test-custom")
}

func TestApplyUnknownNameFallsBackToDefault(t *testing.T) {
	ApplyTheme("no-such-theme")
	if Active.Primary != Themes["default"].Primary {
		t.Error("unknown theme name should fall back to default colors")
	}
}

func TestThemeFromFilePartialOverride(t *testing.T) {
	f := themeFile{
		Name:    "partial",
		Minimal: false,
		Colors:  map[string]string{"primary": "#112233"},
	}
	th := themeFromFile(f)
	if th.Primary != lipgloss.Color("#112233") {
		t.Errorf("primary not set: got %v", th.Primary)
	}
	base := newBaseTheme()
	if th.Fg != base.Fg {
		t.Errorf("unset field Fg should keep base value: got %v, want %v", th.Fg, base.Fg)
	}
}

func TestGradientFieldsPresent(t *testing.T) {
	th := Themes["default"]
	if th.RunningGradientA == nil || th.RunningGradientB == nil {
		t.Error("RunningGradientA/B must be set in default theme")
	}
}

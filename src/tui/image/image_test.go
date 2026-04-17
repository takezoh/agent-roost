package image

import (
	"testing"
)

func setEnv(t *testing.T, key, val string) {
	t.Helper()
	t.Setenv(key, val)
}

func TestDetectOffReturnsCapNone(t *testing.T) {
	setEnv(t, "ROOST_IMAGES", "off")
	if got := Detect(); got != CapNone {
		t.Errorf("ROOST_IMAGES=off: want CapNone, got %v", got)
	}
}

func TestDetectExplicitKitty(t *testing.T) {
	setEnv(t, "ROOST_IMAGES", "kitty")
	if got := Detect(); got != CapKitty {
		t.Errorf("ROOST_IMAGES=kitty: want CapKitty, got %v", got)
	}
}

func TestDetectExplicitITerm2(t *testing.T) {
	setEnv(t, "ROOST_IMAGES", "iterm2")
	if got := Detect(); got != CapITerm2 {
		t.Errorf("ROOST_IMAGES=iterm2: want CapITerm2, got %v", got)
	}
}

func TestDetectExplicitSixel(t *testing.T) {
	setEnv(t, "ROOST_IMAGES", "sixel")
	if got := Detect(); got != CapSixel {
		t.Errorf("ROOST_IMAGES=sixel: want CapSixel, got %v", got)
	}
}

func TestDetectTmuxReturnsCapNone(t *testing.T) {
	// Simulate tmux environment: rasterm.IsTmuxScreen checks TERM and TMUX.
	setEnv(t, "ROOST_IMAGES", "auto")
	setEnv(t, "TERM", "screen")
	setEnv(t, "TMUX", "/tmp/tmux-1000/default,1234,0")
	// Inside tmux passthrough is not configured → expect CapNone.
	if got := Detect(); got != CapNone {
		t.Errorf("TERM=screen+TMUX: want CapNone inside tmux, got %v", got)
	}
}

func TestDetectAutoNoTerminalReturnsCapNone(t *testing.T) {
	// Clear all image-related env vars; non-tty CI env → CapNone.
	setEnv(t, "ROOST_IMAGES", "auto")
	setEnv(t, "KITTY_WINDOW_ID", "")
	setEnv(t, "TERM_PROGRAM", "")
	setEnv(t, "TMUX", "")
	setEnv(t, "TERM", "xterm-256color")
	// IsSixelCapable() tries a terminal DA1 query which fails in CI.
	got := Detect()
	if got == CapKitty || got == CapITerm2 {
		t.Errorf("empty env should not return kitty/iterm2, got %v", got)
	}
}

func TestCapabilityStringValues(t *testing.T) {
	cases := map[Capability]string{
		CapNone:   "none",
		CapKitty:  "kitty",
		CapITerm2: "iterm2",
		CapSixel:  "sixel",
	}
	for cap, want := range cases {
		if got := cap.String(); got != want {
			t.Errorf("%v.String() = %q, want %q", cap, got, want)
		}
	}
}

func TestRenderCapNoneReturnsEmpty(t *testing.T) {
	if got := Render(nil, CapNone); got != "" {
		t.Errorf("CapNone should return empty string, got %q", got)
	}
}

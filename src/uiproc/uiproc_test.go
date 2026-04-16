package uiproc

import (
	"reflect"
	"testing"
)

// uiEqual compares two UIProcess values using reflect.DeepEqual
// because the ExtraArgs slice field prevents direct == comparison.
func uiEqual(a, b UIProcess) bool {
	return reflect.DeepEqual(a, b)
}

// === Constructors ===

func TestConstructorsReturnConsistentValues(t *testing.T) {
	if !uiEqual(Main(), Main()) {
		t.Error("Main() must be idempotent")
	}
	if !uiEqual(Log(), Log()) {
		t.Error("Log() must be idempotent")
	}
	if !uiEqual(Sessions(), Sessions()) {
		t.Error("Sessions() must be idempotent")
	}
}

func TestConstructorFields(t *testing.T) {
	m := Main()
	if m.Name != "main" || m.PaneSuffix != ":0.0" || m.Subcommand != "main" {
		t.Errorf("Main() fields unexpected: %+v", m)
	}
	l := Log()
	if l.Name != "log" || l.PaneSuffix != ":0.1" || l.Subcommand != "log" {
		t.Errorf("Log() fields unexpected: %+v", l)
	}
	s := Sessions()
	if s.Name != "sessions" || s.PaneSuffix != ":0.2" || s.Subcommand != "sessions" {
		t.Errorf("Sessions() fields unexpected: %+v", s)
	}
}

func TestPaletteNoTool(t *testing.T) {
	p := Palette("", nil)
	if p.Name != "palette" || p.Subcommand != "palette" {
		t.Errorf("Palette fields unexpected: %+v", p)
	}
	if len(p.ExtraArgs) != 0 {
		t.Errorf("expected no extra args, got %v", p.ExtraArgs)
	}
}

func TestPaletteWithTool(t *testing.T) {
	p := Palette("shutdown", nil)
	if len(p.ExtraArgs) != 1 || p.ExtraArgs[0] != "--tool='shutdown'" {
		t.Errorf("unexpected ExtraArgs: %v", p.ExtraArgs)
	}
}

func TestPaletteWithArgs(t *testing.T) {
	p := Palette("push-driver", map[string]string{"session_id": "abc123"})
	want := []string{"--tool='push-driver'", "--arg='session_id=abc123'"}
	if !reflect.DeepEqual(p.ExtraArgs, want) {
		t.Errorf("ExtraArgs = %v, want %v", p.ExtraArgs, want)
	}
}

func TestPaletteArgsSkipsEmptyValues(t *testing.T) {
	p := Palette("x", map[string]string{"k": ""})
	// Empty value must be skipped; only --tool= arg present.
	if len(p.ExtraArgs) != 1 {
		t.Errorf("expected 1 extra arg (--tool=), got %v", p.ExtraArgs)
	}
}

func TestPaletteArgsSorted(t *testing.T) {
	p := Palette("", map[string]string{"z": "1", "a": "2", "m": "3"})
	want := []string{"--arg='a=2'", "--arg='m=3'", "--arg='z=1'"}
	if !reflect.DeepEqual(p.ExtraArgs, want) {
		t.Errorf("ExtraArgs = %v, want %v", p.ExtraArgs, want)
	}
}

// === RespawnTarget ===

func TestRespawnTargetControlPanes(t *testing.T) {
	cases := []struct {
		pane string
		want UIProcess
	}{
		{"{sessionName}:0.1", Log()},
		{"{sessionName}:0.2", Sessions()},
	}
	for _, tc := range cases {
		got, ok := RespawnTarget(tc.pane)
		if !ok {
			t.Errorf("RespawnTarget(%q): expected match", tc.pane)
			continue
		}
		if !uiEqual(got, tc.want) {
			t.Errorf("RespawnTarget(%q) = %+v, want %+v", tc.pane, got, tc.want)
		}
	}
}

func TestRespawnTargetMainPaneNotHandled(t *testing.T) {
	_, ok := RespawnTarget("{sessionName}:0.0")
	if ok {
		t.Error("pane 0.0 must not be handled by RespawnTarget (requires active-session check)")
	}
}

func TestRespawnTargetUnknownPane(t *testing.T) {
	_, ok := RespawnTarget("garbage")
	if ok {
		t.Error("unknown pane must return false")
	}
}

// === Command ===

func TestCommandMain(t *testing.T) {
	got := Main().Command("/usr/bin/roost")
	want := "'/usr/bin/roost' --tui main"
	if got != want {
		t.Errorf("Command() = %q, want %q", got, want)
	}
}

func TestCommandWithExtraArgs(t *testing.T) {
	p := Palette("shutdown", nil)
	got := p.Command("/usr/bin/roost")
	want := "'/usr/bin/roost' --tui palette --tool='shutdown'"
	if got != want {
		t.Errorf("Command() = %q, want %q", got, want)
	}
}

func TestCommandShellQuotesExePath(t *testing.T) {
	got := Main().Command("/path with spaces/roost")
	want := "'/path with spaces/roost' --tui main"
	if got != want {
		t.Errorf("Command() = %q, want %q", got, want)
	}
}

func TestShellQuoteEscapesSingleQuote(t *testing.T) {
	// Defensive: a path or value containing a single quote is escaped.
	got := shellQuote("it's")
	want := "'it'\\''s'"
	if got != want {
		t.Errorf("shellQuote = %q, want %q", got, want)
	}
}

package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/take/agent-roost/core"
	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
	"github.com/take/agent-roost/state"
)

func TestTruncate(t *testing.T) {
	tests := []struct {
		s    string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 8, "hello w…"},
		{"αβγδε", 3, "αβ…"},
	}
	for _, tt := range tests {
		got := truncate(tt.s, tt.n)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
		}
	}
}

func TestRenderSession_TagsAndTitle(t *testing.T) {
	registry := driver.DefaultRegistry()
	s := &core.SessionInfo{
		ID:         "abc123",
		Command:    "claude",
		Tags:       []session.Tag{{Text: "main", Background: "#A9DC76"}},
		Title:      "my-session-name",
		LastPrompt: "the last prompt",
		State:      state.StatusWaiting,
		CreatedAt:  time.Now().Add(-3 * time.Minute).Format("2006-01-02T15:04:05Z07:00"),
	}
	out := renderSession(s, false, 60, registry)
	if !strings.Contains(out, "my-session-name") {
		t.Errorf("expected title in output, got:\n%s", out)
	}
	if !strings.Contains(out, "claude") {
		t.Errorf("expected claude tag in output, got:\n%s", out)
	}
	if !strings.Contains(out, "main") {
		t.Errorf("expected main tag in output, got:\n%s", out)
	}
	if !strings.Contains(out, "the last prompt") {
		t.Errorf("expected last prompt in output, got:\n%s", out)
	}
}

func TestRenderSession_NoTitle_ShowsID(t *testing.T) {
	registry := driver.DefaultRegistry()
	s := &core.SessionInfo{
		ID:      "abc123",
		Command: "gemini",
		Tags:    nil,

		Title:     "",
		State:     state.StatusIdle,
		CreatedAt: time.Now().Add(-5 * time.Minute).Format("2006-01-02T15:04:05Z07:00"),
	}
	out := renderSession(s, false, 60, registry)
	if !strings.Contains(out, "abc123") {
		t.Errorf("expected ID in output when no title, got:\n%s", out)
	}
	if !strings.Contains(out, "gemini") {
		t.Errorf("expected gemini tag in output, got:\n%s", out)
	}
}

func TestRenderSession_MinimalTags(t *testing.T) {
	t.Cleanup(func() { ApplyTheme("default") })
	ApplyTheme("minimal")

	registry := driver.DefaultRegistry()
	s := &core.SessionInfo{
		ID:        "abc123",
		Command:   "claude",
		Tags:      []session.Tag{{Text: "main", Background: "#A9DC76"}},
		Title:     "my-session",
		State:     state.StatusIdle,
		CreatedAt: time.Now().Add(-1 * time.Minute).Format("2006-01-02T15:04:05Z07:00"),
	}
	out := stripANSI(renderSession(s, false, 60, registry))
	if !strings.Contains(out, "▸ claude") {
		t.Errorf("expected '▸ claude' prefix in minimal mode, got:\n%s", out)
	}
	if !strings.Contains(out, "⎇ main") {
		t.Errorf("expected '⎇ main' prefix in minimal mode, got:\n%s", out)
	}
}

func TestRenderSession_MinimalSavesRowsVsDefault(t *testing.T) {
	t.Cleanup(func() { ApplyTheme("default") })

	registry := driver.DefaultRegistry()
	s := &core.SessionInfo{
		ID:         "abc123",
		Command:    "claude",
		Tags:       []session.Tag{{Text: "main"}},
		Title:      "my-session",
		LastPrompt: "the last prompt",
		State:      state.StatusIdle,
		CreatedAt:  time.Now().Add(-1 * time.Minute).Format("2006-01-02T15:04:05Z07:00"),
	}

	ApplyTheme("default")
	defRows := strings.Count(renderSession(s, false, 60, registry), "\n") + 1

	ApplyTheme("minimal")
	minRows := strings.Count(renderSession(s, false, 60, registry), "\n") + 1

	if minRows >= defRows {
		t.Errorf("minimal mode should use fewer rows than default; default=%d minimal=%d", defRows, minRows)
	}
	if defRows-minRows != 2 {
		t.Errorf("expected minimal to save exactly 2 rows (no top/bottom border); default=%d minimal=%d", defRows, minRows)
	}
}

func TestRenderSession_MinimalSelectionBar(t *testing.T) {
	t.Cleanup(func() { ApplyTheme("default") })
	ApplyTheme("minimal")

	registry := driver.DefaultRegistry()
	s := &core.SessionInfo{
		ID:        "abc123",
		Command:   "claude",
		Title:     "x",
		State:     state.StatusIdle,
		CreatedAt: time.Now().Format("2006-01-02T15:04:05Z07:00"),
	}

	selected := stripANSI(renderSession(s, true, 60, registry))
	unselected := stripANSI(renderSession(s, false, 60, registry))

	if !strings.Contains(selected, "▌") {
		t.Errorf("selected card should contain '▌' bar, got:\n%s", selected)
	}
	if strings.Contains(unselected, "▌") {
		t.Errorf("unselected card should NOT contain '▌' bar, got:\n%s", unselected)
	}
}

func TestRenderSessionSeparator_MinimalDimRule(t *testing.T) {
	t.Cleanup(func() { ApplyTheme("default") })
	ApplyTheme("minimal")

	out := renderSessionSeparator(60)
	plain := stripANSI(out)
	if !strings.Contains(plain, "─") {
		t.Errorf("separator should contain '─', got: %q", plain)
	}
	// Active.Dim is #626262 → ANSI fg "38;2;98;98;98".
	if !strings.Contains(out, "38;2;98;98;98") {
		t.Errorf("separator should be rendered in Active.Dim, got: %q", out)
	}
	// Should be at least the card width minus 2; check substantial length.
	if dashes := strings.Count(plain, "─"); dashes < 50 {
		t.Errorf("separator should span most of the inner width, got %d dashes", dashes)
	}
}

func TestFormatElapsed(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{0, "0m"},
		{30 * time.Minute, "30m"},
		{59 * time.Minute, "59m"},
		{1 * time.Hour, "1h"},
		{23 * time.Hour, "23h"},
		{24 * time.Hour, "1d"},
		{48 * time.Hour, "2d"},
	}
	for _, tt := range tests {
		if got := formatElapsed(tt.in); got != tt.want {
			t.Errorf("formatElapsed(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/take/agent-roost/core"
	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
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
		Tags:       []session.Tag{{Text: "main", Foreground: "#A9DC76"}},
		Title:      "my-session-name",
		LastPrompt: "the last prompt",
		State:      session.StateWaiting,
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
		ID:        "abc123",
		Command:   "gemini",
		Tags:      nil,

		Title:     "",
		State:     session.StateIdle,
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

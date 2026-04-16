package tui

import (
	"strings"
	"testing"
)

func TestLinkContainsOSC8(t *testing.T) {
	SetHyperlinksActive(true)
	defer SetHyperlinksActive(true)
	got := Link("https://example.com", "click")
	if !strings.Contains(got, "\x1b]8;;https://example.com") {
		t.Errorf("expected OSC 8 open escape, got %q", got)
	}
	// lipgloss terminates OSC with BEL (\a) or ST (\x1b\\); accept either
	hasBEL := strings.Contains(got, "\x1b]8;;\a")
	hasST := strings.Contains(got, "\x1b]8;;\x1b\\")
	if !hasBEL && !hasST {
		t.Errorf("expected OSC 8 close escape, got %q", got)
	}
	if !strings.Contains(got, "click") {
		t.Errorf("expected link text 'click', got %q", got)
	}
}

func TestLinkDisabled(t *testing.T) {
	SetHyperlinksActive(false)
	defer SetHyperlinksActive(true)
	got := Link("https://example.com", "click")
	if got != "click" {
		t.Errorf("expected plain text, got %q", got)
	}
}

func TestLinkEmptyURL(t *testing.T) {
	SetHyperlinksActive(true)
	got := Link("", "text")
	if got != "text" {
		t.Errorf("expected plain text for empty url, got %q", got)
	}
}

func TestFileLink(t *testing.T) {
	got := fileLink("/home/user/project")
	want := "file://localhost/home/user/project"
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

func TestFileLinkEmpty(t *testing.T) {
	if got := fileLink(""); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

package tui

import (
	"strings"
	"testing"

	"github.com/takezoh/agent-roost/proto"
)

func TestRenderInfoContentEmitsOSC8ForProject(t *testing.T) {
	SetHyperlinksActive(true)
	defer SetHyperlinksActive(true)
	s := &proto.SessionInfo{Project: "/home/user/myproject"}
	got := renderInfoContent(s, 80)
	if !strings.Contains(got, "\x1b]8;;file://localhost/home/user/myproject") {
		t.Errorf("expected OSC 8 URL for project path, got %q", got)
	}
}

func TestRenderInfoContentTruncatesLongProject(t *testing.T) {
	SetHyperlinksActive(true)
	defer SetHyperlinksActive(true)
	long := "/home/user/" + strings.Repeat("a", 100)
	s := &proto.SessionInfo{Project: long}
	got := renderInfoContent(s, 40)
	if !strings.Contains(got, "…") {
		t.Errorf("expected ellipsis for truncated project path, got %q", got)
	}
	if !strings.Contains(got, "\x1b]8;;file://localhost"+long) {
		t.Errorf("expected full path in OSC 8 URL even when display is truncated, got %q", got)
	}
}

func TestRenderInfoContentHyperlinksDisabled(t *testing.T) {
	SetHyperlinksActive(false)
	defer SetHyperlinksActive(true)
	s := &proto.SessionInfo{Project: "/home/user/myproject"}
	got := renderInfoContent(s, 80)
	if strings.Contains(got, "\x1b]8;;") {
		t.Errorf("expected no OSC 8 when hyperlinks disabled, got %q", got)
	}
	if !strings.Contains(got, "/home/user/myproject") {
		t.Errorf("expected plain project path when hyperlinks disabled, got %q", got)
	}
}

package driver

import (
	"testing"

	"github.com/takezoh/agent-roost/driver/vt"
)

func TestParseOscNotifOSC9(t *testing.T) {
	title, body := parseOscNotif(vt.OscNotification{Cmd: 9, Payload: "  hello  "})
	if title != "hello" || body != "" {
		t.Errorf("osc9: got (%q, %q), want (hello, \"\")", title, body)
	}
}

func TestParseOscNotifOSC777(t *testing.T) {
	title, body := parseOscNotif(vt.OscNotification{Cmd: 777, Payload: "notify;Build Done;task finished"})
	if title != "Build Done" || body != "task finished" {
		t.Errorf("osc777: got (%q, %q), want (Build Done, task finished)", title, body)
	}
}

func TestParseOscNotifOSC99FullPayload(t *testing.T) {
	payload := "i=1:d=My Title:p=Some body text:f=false:o=always"
	title, body := parseOscNotif(vt.OscNotification{Cmd: 99, Payload: payload})
	if title != "My Title" || body != "Some body text" {
		t.Errorf("osc99 full: got (%q, %q), want (My Title, Some body text)", title, body)
	}
}

func TestParseOscNotifOSC99TitleOnly(t *testing.T) {
	title, body := parseOscNotif(vt.OscNotification{Cmd: 99, Payload: "d=Alert"})
	if title != "Alert" || body != "" {
		t.Errorf("osc99 title only: got (%q, %q), want (Alert, \"\")", title, body)
	}
}

func TestParseOscNotifOSC99BodyOnly(t *testing.T) {
	title, body := parseOscNotif(vt.OscNotification{Cmd: 99, Payload: "p=body text"})
	if title != "" || body != "body text" {
		t.Errorf("osc99 body only: got (%q, %q), want (\"\", body text)", title, body)
	}
}

func TestParseOscNotifOSC99Fallback(t *testing.T) {
	// Payload with no recognised keys falls back to verbatim body.
	title, body := parseOscNotif(vt.OscNotification{Cmd: 99, Payload: "raw verbatim payload"})
	if title != "" || body != "raw verbatim payload" {
		t.Errorf("osc99 fallback: got (%q, %q), want (\"\", raw verbatim payload)", title, body)
	}
}

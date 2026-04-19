package state

import (
	"testing"
)

func TestReducePaneOsc_EmitsRecordNotification(t *testing.T) {
	s := New()
	sessID := SessionID("sess1")
	s.Sessions = map[SessionID]Session{sessID: stubSession(sessID)}
	frameID := FrameID(sessID)

	ev := EvPaneOsc{FrameID: frameID, Cmd: 9, Title: "hello", Body: ""}
	_, effs := Reduce(s, ev)

	if len(effs) != 1 {
		t.Fatalf("expected 1 effect, got %d", len(effs))
	}
	rec, ok := effs[0].(EffRecordNotification)
	if !ok {
		t.Fatalf("expected EffRecordNotification, got %T", effs[0])
	}
	if rec.FrameID != frameID {
		t.Errorf("frameID: got %q, want %q", rec.FrameID, frameID)
	}
	if rec.SessionID != sessID {
		t.Errorf("sessionID: got %q, want %q", rec.SessionID, sessID)
	}
	if rec.Cmd != 9 || rec.Title != "hello" {
		t.Errorf("unexpected content: cmd=%d title=%q", rec.Cmd, rec.Title)
	}
}

func TestReducePaneOsc_EmptyTitleBody_NoEffect(t *testing.T) {
	s := New()
	sessID := SessionID("sess1")
	s.Sessions = map[SessionID]Session{sessID: stubSession(sessID)}
	frameID := FrameID(sessID)

	_, effs := Reduce(s, EvPaneOsc{FrameID: frameID, Cmd: 9, Title: "", Body: ""})
	if len(effs) != 0 {
		t.Errorf("expected no effects for empty notification, got %d", len(effs))
	}
}

func TestReducePaneOsc_UnknownFrame_NoEffect(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvPaneOsc{FrameID: "ghost", Cmd: 9, Title: "hi"})
	if len(effs) != 0 {
		t.Errorf("expected no effects for unknown frame, got %d", len(effs))
	}
}

func TestReducePaneOsc_OSC0_RoutesToDriver_NotRecordNotification(t *testing.T) {
	s := New()
	sessID := SessionID("sess1")
	s.Sessions = map[SessionID]Session{sessID: stubSession(sessID)}
	frameID := FrameID(sessID)

	_, effs := Reduce(s, EvPaneOsc{FrameID: frameID, Cmd: 0, Title: "✳ Claude Code"})

	for _, e := range effs {
		if _, ok := e.(EffRecordNotification); ok {
			t.Error("OSC 0 should not produce EffRecordNotification")
		}
	}
}

func TestReducePaneOsc_OSC0_EmptyTitle_NoEffect(t *testing.T) {
	s := New()
	sessID := SessionID("sess1")
	s.Sessions = map[SessionID]Session{sessID: stubSession(sessID)}
	frameID := FrameID(sessID)

	_, effs := Reduce(s, EvPaneOsc{FrameID: frameID, Cmd: 0, Title: ""})
	if len(effs) != 0 {
		t.Errorf("expected no effects for empty OSC 0 title, got %d", len(effs))
	}
}

func TestReducePaneOsc_OSC9_StillEmitsRecordNotification(t *testing.T) {
	s := New()
	sessID := SessionID("sess1")
	s.Sessions = map[SessionID]Session{sessID: stubSession(sessID)}
	frameID := FrameID(sessID)

	_, effs := Reduce(s, EvPaneOsc{FrameID: frameID, Cmd: 9, Title: "ping"})
	if _, ok := findEff[EffRecordNotification](effs); !ok {
		t.Error("OSC 9 should still produce EffRecordNotification")
	}
}

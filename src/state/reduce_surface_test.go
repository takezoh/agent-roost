package state

import (
	"testing"
)

func TestReduceSurfaceReadTextNotFound(t *testing.T) {
	s := New()
	ev := EvCmdSurfaceReadText{ConnID: 1, ReqID: "r1", SessionID: "no-such", Lines: 10}
	_, effs := Reduce(s, ev)
	if len(effs) != 1 {
		t.Fatalf("expected 1 effect, got %d", len(effs))
	}
	e, ok := effs[0].(EffSendError)
	if !ok {
		t.Fatalf("effect = %T, want EffSendError", effs[0])
	}
	if e.Code != "not_found" {
		t.Errorf("code = %q, want not_found", e.Code)
	}
}

func TestReduceSurfaceReadTextFound(t *testing.T) {
	s := New()
	s.Sessions["sess-1"] = Session{ID: "sess-1"}
	ev := EvCmdSurfaceReadText{ConnID: 1, ReqID: "r1", SessionID: "sess-1", Lines: 20}
	_, effs := Reduce(s, ev)
	if len(effs) != 1 {
		t.Fatalf("expected 1 effect, got %d", len(effs))
	}
	e, ok := effs[0].(EffSendResponseSync)
	if !ok {
		t.Fatalf("effect = %T, want EffSendResponseSync", effs[0])
	}
	reply, ok := e.Body.(SurfaceReadTextReply)
	if !ok {
		t.Fatalf("body = %T, want SurfaceReadTextReply", e.Body)
	}
	if reply.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want sess-1", reply.SessionID)
	}
	if reply.Lines != 20 {
		t.Errorf("Lines = %d, want 20", reply.Lines)
	}
}

func TestReduceSurfaceReadTextDefaultLines(t *testing.T) {
	s := New()
	s.Sessions["sess-1"] = Session{ID: "sess-1"}
	ev := EvCmdSurfaceReadText{ConnID: 1, ReqID: "r1", SessionID: "sess-1", Lines: 0}
	_, effs := Reduce(s, ev)
	reply := effs[0].(EffSendResponseSync).Body.(SurfaceReadTextReply)
	if reply.Lines != 30 {
		t.Errorf("default Lines = %d, want 30", reply.Lines)
	}
}

func TestReduceSurfaceSendText(t *testing.T) {
	s := New()
	s.Sessions["sess-1"] = Session{ID: "sess-1"}
	ev := EvCmdSurfaceSendText{ConnID: 1, ReqID: "r1", SessionID: "sess-1", Text: "hello"}
	_, effs := Reduce(s, ev)
	if len(effs) != 1 {
		t.Fatalf("expected 1 effect, got %d", len(effs))
	}
	e, ok := effs[0].(EffSendTmuxKeys)
	if !ok {
		t.Fatalf("effect = %T, want EffSendTmuxKeys", effs[0])
	}
	if !e.WithEnter {
		t.Error("WithEnter should be true for send_text")
	}
	if e.Text != "hello" {
		t.Errorf("Text = %q, want hello", e.Text)
	}
}

func TestReduceSurfaceSendKey(t *testing.T) {
	s := New()
	s.Sessions["sess-1"] = Session{ID: "sess-1"}
	ev := EvCmdSurfaceSendKey{ConnID: 1, ReqID: "r1", SessionID: "sess-1", Key: "Escape"}
	_, effs := Reduce(s, ev)
	e, ok := effs[0].(EffSendTmuxKeys)
	if !ok {
		t.Fatalf("effect = %T, want EffSendTmuxKeys", effs[0])
	}
	if e.WithEnter {
		t.Error("WithEnter should be false for send_key")
	}
	if e.Key != "Escape" {
		t.Errorf("Key = %q, want Escape", e.Key)
	}
}

func TestReduceDriverList(t *testing.T) {
	s := New()
	ev := EvCmdDriverList{ConnID: 1, ReqID: "r1"}
	_, effs := Reduce(s, ev)
	if len(effs) != 1 {
		t.Fatalf("expected 1 effect, got %d", len(effs))
	}
	e, ok := effs[0].(EffSendResponseSync)
	if !ok {
		t.Fatalf("effect = %T, want EffSendResponseSync", effs[0])
	}
	if _, ok := e.Body.(DriverListReply); !ok {
		t.Fatalf("body = %T, want DriverListReply", e.Body)
	}
}

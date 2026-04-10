package proto

import (
	"encoding/json"
	"testing"
)

func TestEncodeDecodeCommand(t *testing.T) {
	cases := []Command{
		CmdSubscribe{Filters: []string{"sessions-changed"}},
		CmdCreateSession{Project: "/foo", Command: "claude"},
		CmdStopSession{SessionID: "abc"},
		CmdListSessions{},
		CmdPreviewSession{SessionID: "abc"},
		CmdSwitchSession{SessionID: "abc"},
		CmdPreviewProject{Project: "/p"},
		CmdFocusPane{Pane: "0.1"},
		CmdLaunchTool{Tool: "new-session", Args: map[string]string{"project": "/p"}},
		CmdHook{Driver: "claude", Event: "session-start", SessionID: "abc", Payload: map[string]any{"k": "v"}},
		CmdShutdown{},
		CmdDetach{},
	}
	for _, c := range cases {
		t.Run(c.CommandName(), func(t *testing.T) {
			wire, err := EncodeCommand("r1", c)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			env, err := DecodeEnvelope(wire)
			if err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			if env.Type != TypeCommand {
				t.Errorf("type = %q", env.Type)
			}
			if env.ReqID != "r1" {
				t.Errorf("ReqID = %q", env.ReqID)
			}
			if env.Cmd != c.CommandName() {
				t.Errorf("Cmd = %q, want %q", env.Cmd, c.CommandName())
			}
			decoded, err := DecodeCommand(env)
			if err != nil {
				t.Fatalf("decode command: %v", err)
			}
			if decoded.CommandName() != c.CommandName() {
				t.Errorf("decoded name = %q, want %q", decoded.CommandName(), c.CommandName())
			}
		})
	}
}

func TestDecodeCommandUnknown(t *testing.T) {
	env := Envelope{Type: TypeCommand, Cmd: "garbage", Data: json.RawMessage(`{}`)}
	if _, err := DecodeCommand(env); err == nil {
		t.Error("expected error for unknown command")
	}
}

func TestDecodeCommandWrongType(t *testing.T) {
	env := Envelope{Type: TypeEvent}
	if _, err := DecodeCommand(env); err == nil {
		t.Error("expected error for non-command envelope")
	}
}

func TestEncodeDecodeResponse(t *testing.T) {
	r := RespCreateSession{SessionID: "abc", WindowID: "@5"}
	wire, err := EncodeResponse("r1", r)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	env, err := DecodeEnvelope(wire)
	if err != nil {
		t.Fatalf("decode env: %v", err)
	}
	if env.Status != StatusOK {
		t.Errorf("status = %q", env.Status)
	}
	var got RespCreateSession
	if err := DecodeResponse(env, &got); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if got != r {
		t.Errorf("got = %+v, want %+v", got, r)
	}
}

func TestEncodeError(t *testing.T) {
	wire, err := EncodeError("r1", ErrNotFound, "missing", map[string]any{"id": "abc"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	env, _ := DecodeEnvelope(wire)
	if env.Status != StatusError {
		t.Errorf("status = %q", env.Status)
	}
	if env.Error == nil {
		t.Fatal("Error body missing")
	}
	if env.Error.Code != ErrNotFound {
		t.Errorf("code = %q", env.Error.Code)
	}
	if env.Error.Message != "missing" {
		t.Errorf("message = %q", env.Error.Message)
	}
}

func TestEncodeDecodeEvent(t *testing.T) {
	cases := []ServerEvent{
		EvtSessionsChanged{Sessions: []SessionInfo{{ID: "abc"}}, ActiveWindowID: "@5"},
		EvtProjectSelected{Project: "/foo"},
		EvtPaneFocused{Pane: "0.1"},
		EvtLogLine{Path: "/var/log", Line: "hello"},
		EvtTranscriptLine{SessionID: "abc", Line: "world"},
	}
	for _, e := range cases {
		t.Run(e.EventName(), func(t *testing.T) {
			wire, err := EncodeEvent(e)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			env, err := DecodeEnvelope(wire)
			if err != nil {
				t.Fatalf("decode env: %v", err)
			}
			if env.Type != TypeEvent {
				t.Errorf("type = %q", env.Type)
			}
			ev, err := DecodeEvent(env)
			if err != nil {
				t.Fatalf("decode event: %v", err)
			}
			if ev.EventName() != e.EventName() {
				t.Errorf("name = %q, want %q", ev.EventName(), e.EventName())
			}
		})
	}
}

func TestDecodeEventUnknown(t *testing.T) {
	env := Envelope{Type: TypeEvent, Name: "garbage", Data: json.RawMessage(`{}`)}
	if _, err := DecodeEvent(env); err == nil {
		t.Error("expected error for unknown event")
	}
}

func TestErrCodeFromState(t *testing.T) {
	cases := map[string]ErrCode{
		"not_found":        ErrNotFound,
		"invalid_argument": ErrInvalidArgument,
		"internal":         ErrInternal,
		"session_stopped":  ErrSessionStopped,
		"already_exists":   ErrAlreadyExists,
		"unsupported":      ErrUnsupported,
		"random":           ErrUnknown,
	}
	for in, want := range cases {
		if got := FromStateCode(in); got != want {
			t.Errorf("FromStateCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestErrorBodyImplementsError(t *testing.T) {
	e := &ErrorBody{Code: ErrNotFound, Message: "missing"}
	var err error = e
	if err.Error() != "not_found: missing" {
		t.Errorf("Error() = %q", err.Error())
	}
}

func TestReqIDGenUnique(t *testing.T) {
	g := NewReqIDGen()
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := g.Next()
		if seen[id] {
			t.Errorf("collision at %d: %q", i, id)
		}
		seen[id] = true
	}
}

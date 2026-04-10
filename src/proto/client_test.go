package proto

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/take/agent-roost/state"
)

// fakeServer pairs a net.Pipe end with a goroutine that reads
// commands from the client and replies with whatever the test
// scripted via Reply.
type fakeServer struct {
	t      *testing.T
	conn   net.Conn
	reader *bufio.Reader
	writer *bufio.Writer
	wg     sync.WaitGroup
}

func newFakeServer(t *testing.T) (*Client, *fakeServer) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	c := &Client{
		conn:    clientConn,
		writer:  bufio.NewWriter(clientConn),
		gen:     NewReqIDGen(),
		pending: map[string]chan inFlight{},
		events:  make(chan ServerEvent, 16),
		closed:  make(chan struct{}),
	}
	go c.read()
	s := &fakeServer{
		t:      t,
		conn:   serverConn,
		reader: bufio.NewReader(serverConn),
		writer: bufio.NewWriter(serverConn),
	}
	return c, s
}

func (s *fakeServer) recv() Envelope {
	line, err := s.reader.ReadBytes('\n')
	if err != nil {
		s.t.Fatalf("server recv: %v", err)
	}
	env, err := DecodeEnvelope(line)
	if err != nil {
		s.t.Fatalf("server decode: %v", err)
	}
	return env
}

func (s *fakeServer) send(payload []byte) {
	if _, err := s.writer.Write(payload); err != nil {
		s.t.Fatalf("server send: %v", err)
	}
	s.writer.WriteByte('\n')
	s.writer.Flush()
}

func TestClientSendRoundTrip(t *testing.T) {
	c, server := newFakeServer(t)
	defer c.Close()

	type result struct {
		resp Response
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		resp, err := c.Send(ctx, CmdCreateSession{Project: "/foo", Command: "claude"})
		resCh <- result{resp, err}
	}()

	env := server.recv()
	if env.Type != TypeCommand {
		t.Errorf("type = %q", env.Type)
	}
	wire, _ := EncodeResponse(env.ReqID, RespCreateSession{SessionID: "abc", WindowID: "@5"})
	server.send(wire)

	r := <-resCh
	if r.err != nil {
		t.Fatalf("Send error: %v", r.err)
	}
	got, ok := r.resp.(RespCreateSession)
	if !ok {
		t.Fatalf("resp type = %T", r.resp)
	}
	if got.SessionID != "abc" || got.WindowID != "@5" {
		t.Errorf("got = %+v", got)
	}
}

func TestClientErrorResponse(t *testing.T) {
	c, server := newFakeServer(t)
	defer c.Close()

	resCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, err := c.Send(ctx, CmdStopSession{SessionID: "ghost"})
		resCh <- err
	}()

	env := server.recv()
	wire, _ := EncodeError(env.ReqID, ErrNotFound, "missing", nil)
	server.send(wire)

	err := <-resCh
	if err == nil {
		t.Fatal("expected error")
	}
	var ebody *ErrorBody
	if !errors.As(err, &ebody) {
		t.Fatalf("err type = %T, want *ErrorBody", err)
	}
	if ebody.Code != ErrNotFound {
		t.Errorf("code = %q", ebody.Code)
	}
}

func TestClientReceivesEvents(t *testing.T) {
	c, server := newFakeServer(t)
	defer c.Close()

	wire, _ := EncodeEvent(EvtSessionsChanged{
		Sessions:       []SessionInfo{{ID: "abc"}},
		ActiveWindowID: "@5",
	})
	server.send(wire)

	select {
	case ev := <-c.Events():
		got, ok := ev.(EvtSessionsChanged)
		if !ok {
			t.Fatalf("event type = %T", ev)
		}
		if got.ActiveWindowID != "@5" || len(got.Sessions) != 1 {
			t.Errorf("event = %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestClientCloseUnblocksPending(t *testing.T) {
	c, _ := newFakeServer(t)

	resCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, err := c.Send(ctx, CmdShutdown{})
		resCh <- err
	}()

	time.Sleep(20 * time.Millisecond) // let Send register the pending entry
	c.Close()

	select {
	case err := <-resCh:
		if err == nil {
			t.Error("expected error after close")
		}
	case <-time.After(time.Second):
		t.Fatal("Send did not unblock after close")
	}
}

func TestCommandToStateEvent(t *testing.T) {
	cases := []struct {
		cmd  Command
		want string
	}{
		{CmdSubscribe{}, "EvCmdSubscribe"},
		{CmdCreateSession{Project: "/p"}, "EvCmdCreateSession"},
		{CmdStopSession{SessionID: "x"}, "EvCmdStopSession"},
		{CmdHook{Driver: "claude"}, "EvCmdHook"},
		{CmdShutdown{}, "EvCmdShutdown"},
	}
	for _, c := range cases {
		ev := CommandToStateEvent(state.ConnID(1), "r1", c.cmd)
		if ev == nil {
			t.Errorf("nil event for %T", c.cmd)
		}
	}
}

func TestDecodeResponseByCommandHeuristics(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{
			name: "create-session",
			data: mustMarshal(RespCreateSession{SessionID: "x", WindowID: "@1"}),
			want: "RespCreateSession",
		},
		{
			name: "sessions",
			data: mustMarshal(RespSessions{Sessions: []SessionInfo{}}),
			want: "RespSessions",
		},
		{
			name: "active-window",
			data: mustMarshal(RespActiveWindow{ActiveWindowID: "@1"}),
			want: "RespActiveWindow",
		},
		{
			name: "empty",
			data: nil,
			want: "RespOK",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := Envelope{Type: TypeResponse, Status: StatusOK, Data: tc.data}
			r, err := decodeResponseByCommand(env)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			gotName := typeName(r)
			if gotName != tc.want {
				t.Errorf("got %q, want %q", gotName, tc.want)
			}
		})
	}
}

func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func typeName(r Response) string {
	switch r.(type) {
	case RespOK:
		return "RespOK"
	case RespCreateSession:
		return "RespCreateSession"
	case RespSessions:
		return "RespSessions"
	case RespActiveWindow:
		return "RespActiveWindow"
	}
	return "unknown"
}

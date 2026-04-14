package proto

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"testing"

	"github.com/takezoh/agent-roost/state"
	"time"

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
		resp, err := c.Send(ctx, CmdEvent{Event: state.EventCreateSession, Payload: json.RawMessage(`{"project":"/foo","command":"claude"}`)})
		resCh <- result{resp, err}
	}()

	env := server.recv()
	if env.Type != TypeCommand {
		t.Errorf("type = %q", env.Type)
	}
	wire, _ := EncodeResponse(env.ReqID, RespCreateSession{SessionID: "abc"})
	server.send(wire)

	r := <-resCh
	if r.err != nil {
		t.Fatalf("Send error: %v", r.err)
	}
	got, ok := r.resp.(RespCreateSession)
	if !ok {
		t.Fatalf("resp type = %T", r.resp)
	}
	if got.SessionID != "abc" {
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
		_, err := c.Send(ctx, CmdEvent{Event: state.EventStopSession, Payload: json.RawMessage(`{"session_id":"ghost"}`)})
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
		Sessions:        []SessionInfo{{ID: "abc"}},
		ActiveSessionID: "abc",
	})
	server.send(wire)

	select {
	case ev := <-c.Events():
		got, ok := ev.(EvtSessionsChanged)
		if !ok {
			t.Fatalf("event type = %T", ev)
		}
		if got.ActiveSessionID != "abc" || len(got.Sessions) != 1 {
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
		_, err := c.Send(ctx, CmdEvent{Event: state.EventShutdown})
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

func TestDecodeResponseByCommandHeuristics(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{
			name: "create-session",
			data: mustMarshal(RespCreateSession{SessionID: "x"}),
			want: "RespCreateSession",
		},
		{
			name: "sessions",
			data: mustMarshal(RespSessions{Sessions: []SessionInfo{}}),
			want: "RespSessions",
		},
		{
			name: "active-session",
			data: mustMarshal(RespActiveSession{ActiveSessionID: "abc"}),
			want: "RespActiveSession",
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
			r, err := DecodeResponseByCommand(env)
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

// TestPushDriverDecodesCreateSessionReply verifies Fix C: PushDriver expects a
// CreateSessionReply from the daemon (not RespOK) and returns nil on success.
func TestPushDriverDecodesCreateSessionReply(t *testing.T) {
	c, server := newFakeServer(t)
	defer c.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- c.PushDriver("sess-1", "shell")
	}()

	env := server.recv()
	wire, _ := EncodeResponse(env.ReqID, RespCreateSession{SessionID: "sess-1"})
	server.send(wire)

	if err := <-errCh; err != nil {
		t.Fatalf("PushDriver returned error: %v", err)
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
	case RespActiveSession:
		return "RespActiveSession"
	}
	return "unknown"
}

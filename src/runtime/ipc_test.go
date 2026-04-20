package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/state"
)

// startRuntimeWithIPC spins up a Runtime, opens a Unix socket in a
// temp dir, and returns the runtime + the socket path. Caller is
// responsible for cancelling the context to stop the loop.
func startRuntimeWithIPC(t *testing.T, ctx context.Context) (*Runtime, string) { //nolint:unparam
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "roost.sock")
	r := New(Config{
		SessionName:  "roost-test",
		RoostExe:     "/usr/bin/roost",
		DataDir:      dir,
		TickInterval: 10 * time.Second,
		Tmux:         newFakeTmux(),
	})
	go func() {
		_ = r.Run(ctx)
	}()
	if err := r.StartIPC(sock); err != nil {
		t.Fatalf("StartIPC: %v", err)
	}
	return r, sock
}

func dialClient(t *testing.T, sock string) *proto.Client {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := proto.Dial(sock)
		if err == nil {
			return c
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("dial %s timed out", sock)
	return nil
}

func TestIPCListSessionsRoundTrip(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, sock := startRuntimeWithIPC(t, ctx)

	c := dialClient(t, sock)
	defer c.Close()

	rctx, rcancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer rcancel()
	resp, err := c.Send(rctx, proto.CmdEvent{Event: state.EventListSessions})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	sessions, ok := resp.(proto.RespSessions)
	if !ok {
		t.Fatalf("resp type = %T, want RespSessions", resp)
	}
	if len(sessions.Sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions.Sessions))
	}
}

func TestIPCStopUnknownSessionReturnsError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, sock := startRuntimeWithIPC(t, ctx)

	c := dialClient(t, sock)
	defer c.Close()

	rctx, rcancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer rcancel()
	_, err := c.Send(rctx, proto.CmdEvent{Event: state.EventStopSession, Payload: json.RawMessage(`{"session_id":"ghost"}`)})
	if err == nil {
		t.Fatal("expected error for unknown session")
	}
	var ebody *proto.ErrorBody
	if !errors.As(err, &ebody) {
		t.Fatalf("err type = %T", err)
	}
	if ebody.Code != proto.ErrNotFound {
		t.Errorf("code = %q, want not_found", ebody.Code)
	}
}

func TestIPCSubscribeReceivesInitialSessionsChanged(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, sock := startRuntimeWithIPC(t, ctx)

	c := dialClient(t, sock)
	defer c.Close()

	rctx, rcancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer rcancel()
	if _, err := c.Send(rctx, proto.CmdSubscribe{}); err != nil {
		t.Fatalf("Send subscribe: %v", err)
	}

	select {
	case ev := <-c.Events():
		if ev.EventName() != proto.EvtNameSessionsChanged {
			t.Errorf("event = %q, want sessions-changed", ev.EventName())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event received")
	}
}

func TestIPCDecodeUnknownCommandReturnsError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, sock := startRuntimeWithIPC(t, ctx)

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send a malformed envelope (unknown cmd).
	bad := []byte(`{"type":"cmd","req_id":"r1","cmd":"garbage","data":{}}` + "\n")
	if _, err := conn.Write(bad); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	env, err := proto.DecodeEnvelope(buf[:n])
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Status != proto.StatusError {
		t.Errorf("status = %q, want error", env.Status)
	}
	if env.Error == nil || env.Error.Code != proto.ErrInvalidArgument {
		t.Errorf("err = %+v", env.Error)
	}
}

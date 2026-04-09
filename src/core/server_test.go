package core

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// startTestServer starts a Server with no Coordinator/tmux dependencies.
// Tests interact with the actor and per-client outbox directly via the
// in-package helpers below — they never invoke dispatch handlers that
// would touch the nil coordinator.
func startTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "test.sock")
	srv := NewServer(nil, nil, sock)
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	t.Cleanup(srv.Stop)
	return srv, sock
}

// dialAndEnable opens a unix socket connection to the test server and
// waits for the actor to register exactly `expectedTotal` clients, then
// flips the most recent client's broadcastEnabled flag inside the actor
// goroutine. Bypasses the subscribe handler so tests do not need a real
// Coordinator.
//
// `expectedTotal` is the number of clients the test has dialed so far,
// including this one. Without it the loop has a race: handleConn's
// addClient submission can be queued behind the wait loop's exec,
// causing the test to enable a previously-registered client instead of
// the new one.
func dialAndEnable(t *testing.T, srv *Server, sock string, expectedTotal int) (net.Conn, *json.Decoder) {
	t.Helper()
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for {
		var enabled bool
		srv.exec(func() {
			if len(srv.clients) >= expectedTotal {
				cc := srv.clients[expectedTotal-1]
				cc.broadcastEnabled = true
				enabled = true
			}
		})
		if enabled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("client #%d never registered with the actor", expectedTotal)
		}
		time.Sleep(5 * time.Millisecond)
	}
	return conn, json.NewDecoder(conn)
}

func TestServer_BroadcastReachesAllSubscribers(t *testing.T) {
	srv, sock := startTestServer(t)

	connA, decA := dialAndEnable(t, srv, sock, 1)
	defer connA.Close()
	connB, decB := dialAndEnable(t, srv, sock, 2)
	defer connB.Close()

	srv.broadcast(NewEvent("test-event"))

	for name, c := range map[string]struct {
		conn net.Conn
		dec  *json.Decoder
	}{"A": {connA, decA}, "B": {connB, decB}} {
		c := c
		c.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		var msg Message
		if err := c.dec.Decode(&msg); err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if msg.Event != "test-event" {
			t.Errorf("client %s got event = %q, want test-event", name, msg.Event)
		}
	}
}

// TestServer_SlowClientDoesNotBlockBroadcast verifies the per-client
// outbox prevents one stalled subscriber from holding up the others.
// Client A never reads its outbox; client B must still receive each
// broadcast within the test's deadline.
func TestServer_SlowClientDoesNotBlockBroadcast(t *testing.T) {
	srv, sock := startTestServer(t)

	connA, _ := dialAndEnable(t, srv, sock, 1)
	defer connA.Close()

	connB, decB := dialAndEnable(t, srv, sock, 2)
	defer connB.Close()

	const n = clientOutboxSize + 5
	for i := 0; i < n; i++ {
		srv.broadcast(NewEvent("flood"))
	}

	connB.SetReadDeadline(time.Now().Add(3 * time.Second))
	count := 0
	for count < n {
		var msg Message
		if err := decB.Decode(&msg); err != nil {
			break
		}
		if msg.Event == "flood" {
			count++
		}
	}
	if count != n {
		t.Errorf("client B received %d/%d broadcasts while A was stalled", count, n)
	}
}

// TestServer_StopReleasesClients verifies the actor cleanly tears down
// every clientConn when Stop is called and removes the socket file.
func TestServer_StopReleasesClients(t *testing.T) {
	srv, sock := startTestServer(t)

	conn, _ := dialAndEnable(t, srv, sock, 1)
	defer conn.Close()

	srv.Stop()

	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Errorf("socket file still exists after Stop: %v", err)
	}
}

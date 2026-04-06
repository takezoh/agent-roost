package core

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func startTestServer(t *testing.T) (string, net.Listener) {
	t.Helper()
	sockPath := filepath.Join(t.TempDir(), "test.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ln.Close()
		os.Remove(sockPath)
	})
	return sockPath, ln
}

func TestSubscribeConsumesResponse(t *testing.T) {
	sockPath, ln := startTestServer(t)

	// Server goroutine: handle subscribe then list-sessions
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		dec := json.NewDecoder(conn)
		enc := json.NewEncoder(conn)

		// Read subscribe command
		var msg1 Message
		if err := dec.Decode(&msg1); err != nil {
			t.Errorf("decode subscribe: %v", err)
			return
		}
		if msg1.Command != "subscribe" {
			t.Errorf("expected subscribe, got %s", msg1.Command)
			return
		}
		// Send empty response
		enc.Encode(Message{Type: "response"})

		// Read list-sessions command
		var msg2 Message
		if err := dec.Decode(&msg2); err != nil {
			t.Errorf("decode list-sessions: %v", err)
			return
		}
		if msg2.Command != "list-sessions" {
			t.Errorf("expected list-sessions, got %s", msg2.Command)
			return
		}
		// Send response with sessions
		enc.Encode(Message{
			Type: "response",
			Sessions: []SessionInfo{
				{ID: "abc123", Project: "/tmp/proj", Command: "claude", WindowID: "@1"},
			},
		})
	}()

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.StartListening()

	// Subscribe should consume its own response
	client.Subscribe()

	// ListSessions should get the correct response (not the subscribe response)
	sessions, err := client.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].ID != "abc123" {
		t.Fatalf("expected abc123, got %s", sessions[0].ID)
	}
}

func TestEventsChannelCloseOnDisconnect(t *testing.T) {
	sockPath, ln := startTestServer(t)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		// Close immediately to simulate server shutdown
		conn.Close()
	}()

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.StartListening()

	// Events channel should close when connection is broken
	_, ok := <-client.Events()
	if ok {
		t.Fatal("expected events channel to be closed")
	}
}

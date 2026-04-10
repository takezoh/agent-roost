package core

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
)

// TestServerCoordinator_SubscribeReceivesViewPayload is the end-to-end
// regression test for "session list status / lastPrompt / TRANSCRIPT /
// EVENTS stopped working". It wires Server + Coordinator with real
// claude drivers, subscribes a Client over the unix socket, fires hook
// events through the IPC layer, and verifies the resulting
// sessions-changed broadcast carries the populated SessionInfo.View.
func TestServerCoordinator_SubscribeReceivesViewPayload(t *testing.T) {
	dataDir := t.TempDir()
	eventLogDir := filepath.Join(dataDir, "events")

	tmuxStub := &stubTmuxClient{}
	sessions := session.NewSessionService(tmuxStub, dataDir)
	drivers := driver.NewDriverService(driver.DefaultRegistry(), driver.Deps{
		EventLogDir:   eventLogDir,
		IdleThreshold: time.Second,
	})
	coord := NewCoordinator(sessions, drivers, &stubPanes{}, nil, "roost", "")

	// Use a long tick interval so background ticks do not race the test.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	coord.Start(ctx, time.Hour)
	defer coord.Shutdown()

	sock := filepath.Join(dataDir, "test.sock")
	srv := NewServer(coord, nil, sock)
	coord.SetSessionsChangedNotifier(srv.AsyncBroadcast)
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	t.Cleanup(srv.Stop)

	// Pre-create a session BEFORE the client subscribes so the initial
	// snapshot delivered on subscribe should already include it.
	sessID, err := coord.Create("/proj", "claude")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Connect a Client over the real unix socket and subscribe.
	cli, err := Dial(sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cli.Close()
	cli.StartListening()
	cli.Subscribe()

	// Drain the initial sessions-changed event delivered after subscribe.
	select {
	case msg := <-cli.Events():
		if msg.Event != "sessions-changed" {
			t.Fatalf("first event = %q, want sessions-changed", msg.Event)
		}
		if len(msg.Sessions) != 1 {
			t.Fatalf("initial snapshot has %d sessions, want 1", len(msg.Sessions))
		}
		info := msg.Sessions[0]
		if info.ID != sessID {
			t.Errorf("initial snapshot id = %q, want %q", info.ID, sessID)
		}
		if info.State != driver.StatusIdle {
			t.Errorf("initial state = %v, want StatusIdle", info.State)
		}
		var hasEvents bool
		for _, lt := range info.View.LogTabs {
			if lt.Label == "EVENTS" {
				hasEvents = true
			}
		}
		if !hasEvents {
			t.Errorf("initial snapshot missing EVENTS tab; LogTabs = %+v", info.View.LogTabs)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("never received initial sessions-changed event after subscribe")
	}

	// Fire a state-change hook event via the IPC layer (just like
	// `roost claude event` would). The Coordinator should consume it
	// and broadcast a new sessions-changed event with State=Running.
	if err := cli.SendAgentEvent(driver.AgentEvent{
		Type:      driver.AgentEventStateChange,
		SessionID: sessID,
		State:     "running",
	}); err != nil {
		t.Fatalf("agent-event: %v", err)
	}

	// Wait for the post-event broadcast.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("never received sessions-changed after state-change")
		}
		select {
		case msg := <-cli.Events():
			if msg.Event != "sessions-changed" {
				continue
			}
			if len(msg.Sessions) != 1 {
				t.Fatalf("post-event snapshot has %d sessions, want 1", len(msg.Sessions))
			}
			if msg.Sessions[0].State != driver.StatusRunning {
				continue // wait for the running update specifically
			}
			return // success
		case <-time.After(100 * time.Millisecond):
		}
	}
}

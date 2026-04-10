package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
)

// TestCoordinator_SessionInfoCarriesDriverView is a regression test for
// the user-visible "session list status / lastPrompt / TRANSCRIPT /
// EVENTS stopped working" report. It exercises the full data path:
//
//   1. Create a session (claude driver)
//   2. Capture initial SessionInfo via AllSessionInfos
//   3. Verify the View contains the EVENTS log tab (claude always
//      produces one when EventLogDir + sessionID are wired up) and
//      that Status reports Idle (the spawn default).
//   4. Send a hook event (state-change → running) via HandleHookEvent
//   5. Verify the next AllSessionInfos reflects the new state.
func TestCoordinator_SessionInfoCarriesDriverView(t *testing.T) {
	dataDir := t.TempDir()
	eventLogDir := filepath.Join(dataDir, "events")

	tmuxStub := &stubTmuxClient{}
	sessions := session.NewSessionService(tmuxStub, dataDir)
	drivers := driver.NewDriverService(driver.DefaultRegistry(), driver.Deps{
		EventLogDir: eventLogDir,
		IdleThreshold: time.Second,
	})
	c := NewCoordinator(sessions, drivers, &stubPanes{}, nil, "roost", "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Start(ctx, time.Hour) // Disable ticking; the test drives state directly.
	defer c.Shutdown()

	id, err := c.Create("/proj", "claude")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Initial snapshot — no hook event yet, no transcript path.
	infos := c.AllSessionInfos()
	if len(infos) != 1 {
		t.Fatalf("AllSessionInfos = %d, want 1", len(infos))
	}
	info := infos[0]
	if info.ID != id {
		t.Errorf("info.ID = %q, want %q", info.ID, id)
	}

	// Status: spawn default is Idle.
	if info.State != driver.StatusIdle {
		t.Errorf("info.State = %v, want StatusIdle", info.State)
	}

	// View must carry the command tag at minimum.
	if len(info.View.Card.Tags) == 0 || info.View.Card.Tags[0].Text != "claude" {
		t.Errorf("info.View.Card.Tags = %+v, want first=claude", info.View.Card.Tags)
	}

	// EVENTS tab must always be present for claude when eventLogDir +
	// sessionID are configured. This is the regression check for the
	// "EVENTS stopped working" report.
	var hasEvents bool
	for _, lt := range info.View.LogTabs {
		if lt.Label == "EVENTS" {
			hasEvents = true
			expected := filepath.Join(eventLogDir, id+".log")
			if lt.Path != expected {
				t.Errorf("EVENTS path = %q, want %q", lt.Path, expected)
			}
		}
	}
	if !hasEvents {
		t.Errorf("EVENTS log tab missing from View; LogTabs = %+v", info.View.LogTabs)
	}

	// Now drive a hook event: SessionStart with a transcript path so
	// the next View should also carry the TRANSCRIPT tab.
	transcriptPath := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := writeFile(transcriptPath, ""); err != nil {
		t.Fatalf("seed transcript: %v", err)
	}
	sessID, _ := c.HandleHookEvent(driver.AgentEvent{
		Type:      driver.AgentEventSessionStart,
		SessionID: id,
		DriverState: map[string]string{
			"session_id":      "claude-conv-id",
			"transcript_path": transcriptPath,
		},
	})
	if sessID != id {
		t.Errorf("HandleHookEvent returned %q, want %q", sessID, id)
	}

	// State change → running.
	c.HandleHookEvent(driver.AgentEvent{
		Type:      driver.AgentEventStateChange,
		SessionID: id,
		State:     "running",
	})

	infos = c.AllSessionInfos()
	if len(infos) != 1 {
		t.Fatalf("AllSessionInfos after events = %d, want 1", len(infos))
	}
	info = infos[0]

	if info.State != driver.StatusRunning {
		t.Errorf("after state-change, info.State = %v, want StatusRunning", info.State)
	}

	var hasTranscript bool
	for _, lt := range info.View.LogTabs {
		if lt.Label == "TRANSCRIPT" {
			hasTranscript = true
			if lt.Path != transcriptPath {
				t.Errorf("TRANSCRIPT path = %q, want %q", lt.Path, transcriptPath)
			}
		}
	}
	if !hasTranscript {
		t.Errorf("TRANSCRIPT log tab missing after SessionStart; LogTabs = %+v", info.View.LogTabs)
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// TestCoordinator_WarmRestartPopulatesView reproduces the user-reported
// regression by simulating a warm-restart that loads sessions with full
// PersistedState (status, transcript_path, working_dir, etc.) — exactly
// what /home/take/.config/roost/sessions.json holds in production. The
// test then subscribes a Client and verifies the very first
// sessions-changed broadcast carries the populated View (status,
// TRANSCRIPT + EVENTS tabs, branch tag, etc.).
func TestCoordinator_WarmRestartPopulatesView(t *testing.T) {
	dataDir := t.TempDir()
	eventLogDir := filepath.Join(dataDir, "events")

	// Seed a transcript file so refreshMeta has something to read.
	transcriptDir := t.TempDir()
	transcriptPath := filepath.Join(transcriptDir, "claude-conv.jsonl")
	transcriptBody := `{"type":"custom-title","customTitle":"Restored title"}
{"type":"user","uuid":"u1","parentUuid":null,"message":{"content":"the restored prompt"}}
`
	if err := os.WriteFile(transcriptPath, []byte(transcriptBody), 0o644); err != nil {
		t.Fatalf("seed transcript: %v", err)
	}

	// Pre-populate the SessionService with a session that has the same
	// shape as a real warm-restart entry (matches the production
	// sessions.json structure).
	tmuxStub := &stubTmuxClient{}
	tmuxStub.windows = []session.RoostWindow{
		{
			ID:       "9e043a",
			Project:  "/workspace/agent-roost",
			Command:  "claude",
			WindowID: "@20",
			PersistedState: encodeJSON(t, map[string]string{
				"session_id":        "claude-conv",
				"working_dir":       "/workspace/agent-roost",
				"transcript_path":   transcriptPath,
				"status":            "waiting",
				"status_changed_at": "2026-04-09T19:10:42Z",
				"branch_tag":        "main",
				"branch_target":     "/workspace/agent-roost",
				"branch_at":         "2026-04-09T19:00:58Z",
			}),
			CreatedAt: "2026-04-09T18:53:57Z",
		},
	}
	sessions := session.NewSessionService(tmuxStub, dataDir)
	drivers := driver.NewDriverService(driver.DefaultRegistry(), driver.Deps{
		EventLogDir:   eventLogDir,
		IdleThreshold: time.Second,
	})
	coord := NewCoordinator(sessions, drivers, &stubPanes{}, nil, "roost", "")

	// Init phase: warm restart loads sessions and restores drivers.
	if err := coord.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	coord.Start(ctx, time.Hour)
	defer coord.Shutdown()

	// Direct snapshot — no client, no broadcast — so we exercise the
	// AllSessionInfos path on the actor.
	infos := coord.AllSessionInfos()
	if len(infos) != 1 {
		t.Fatalf("AllSessionInfos = %d, want 1", len(infos))
	}
	info := infos[0]
	if info.ID != "9e043a" {
		t.Errorf("info.ID = %q, want 9e043a", info.ID)
	}
	if info.State != driver.StatusWaiting {
		t.Errorf("info.State = %v, want StatusWaiting (restored from persisted_state)", info.State)
	}
	if info.View.Card.Title != "Restored title" {
		t.Errorf("View.Card.Title = %q, want %q (RestorePersistedState should call refreshMeta)",
			info.View.Card.Title, "Restored title")
	}
	if info.View.Card.Subtitle != "the restored prompt" {
		t.Errorf("View.Card.Subtitle = %q, want %q",
			info.View.Card.Subtitle, "the restored prompt")
	}
	var hasTranscript, hasEvents bool
	for _, lt := range info.View.LogTabs {
		switch lt.Label {
		case "TRANSCRIPT":
			hasTranscript = true
			if lt.Path != transcriptPath {
				t.Errorf("TRANSCRIPT path = %q, want %q", lt.Path, transcriptPath)
			}
		case "EVENTS":
			hasEvents = true
		}
	}
	if !hasTranscript {
		t.Errorf("TRANSCRIPT log tab missing after warm restart; LogTabs = %+v", info.View.LogTabs)
	}
	if !hasEvents {
		t.Errorf("EVENTS log tab missing after warm restart; LogTabs = %+v", info.View.LogTabs)
	}
}

func encodeJSON(t *testing.T, m map[string]string) string {
	t.Helper()
	data, err := encodeJSONImpl(m)
	if err != nil {
		t.Fatalf("encodeJSON: %v", err)
	}
	return data
}

func encodeJSONImpl(m map[string]string) (string, error) {
	if len(m) == 0 {
		return "", nil
	}
	out := "{"
	first := true
	for k, v := range m {
		if !first {
			out += ","
		}
		first = false
		out += `"` + k + `":"` + v + `"`
	}
	out += "}"
	return out, nil
}

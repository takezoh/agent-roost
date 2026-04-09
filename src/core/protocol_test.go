package core

import (
	"testing"
	"time"

	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
	"github.com/take/agent-roost/state"
)

func TestSessionInfoDisplayCommand(t *testing.T) {
	si := SessionInfo{Command: "build"}
	if got := si.DisplayCommand(); got != "build" {
		t.Errorf("got %q, want %q", got, "build")
	}
	si = SessionInfo{}
	if got := si.DisplayCommand(); got != "idle" {
		t.Errorf("got %q, want %q", got, "idle")
	}
}

func TestSessionInfoName(t *testing.T) {
	si := SessionInfo{Project: "/home/user/project"}
	if got := si.Name(); got != "project" {
		t.Errorf("got %q, want %q", got, "project")
	}
}

func TestSessionInfoCreatedAtTime(t *testing.T) {
	si := SessionInfo{CreatedAt: "2024-01-15T10:30:00Z"}
	got := si.CreatedAtTime()
	want := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}

	si = SessionInfo{CreatedAt: "invalid"}
	if got := si.CreatedAtTime(); !got.IsZero() {
		t.Errorf("got %v, want zero time", got)
	}
}

func TestNewCommand(t *testing.T) {
	args := map[string]string{"id": "abc"}
	msg := NewCommand("stop", args)
	if msg.Type != "command" {
		t.Errorf("type = %q, want %q", msg.Type, "command")
	}
	if msg.Command != "stop" {
		t.Errorf("command = %q, want %q", msg.Command, "stop")
	}
	if msg.Args["id"] != "abc" {
		t.Errorf("args[id] = %q, want %q", msg.Args["id"], "abc")
	}
}

func TestNewEvent(t *testing.T) {
	msg := NewEvent("refresh")
	if msg.Type != "event" {
		t.Errorf("type = %q, want %q", msg.Type, "event")
	}
	if msg.Event != "refresh" {
		t.Errorf("event = %q, want %q", msg.Event, "refresh")
	}
}

type stubStore struct {
	data map[string]state.Info
}

func (s stubStore) Get(windowID string) (state.Info, bool) {
	info, ok := s.data[windowID]
	return info, ok
}
func (s stubStore) Set(windowID string, info state.Info) error { s.data[windowID] = info; return nil }
func (s stubStore) Delete(windowID string) error               { delete(s.data, windowID); return nil }
func (s stubStore) Snapshot() map[string]state.Info {
	out := make(map[string]state.Info, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out
}
func (s stubStore) LoadFromTmux(reader state.OptionReader) error { return nil }

func TestBuildSessionInfos(t *testing.T) {
	created := time.Date(2024, 3, 10, 12, 0, 0, 0, time.UTC)
	sessions := []*session.Session{{
		ID: "s1", Project: "/tmp/proj", Command: "test",
		WindowID: "w1", CreatedAt: created,
	}}
	agents := driver.NewAgentStore()
	states := stubStore{data: map[string]state.Info{
		"w1": {Status: state.StatusRunning, ChangedAt: created},
	}}
	infos := BuildSessionInfos(sessions, agents, states)
	if len(infos) != 1 {
		t.Fatalf("len = %d, want 1", len(infos))
	}
	info := infos[0]
	if info.ID != "s1" || info.Project != "/tmp/proj" || info.Command != "test" {
		t.Errorf("unexpected fields: %+v", info)
	}
	if info.WindowID != "w1" || info.State != state.StatusRunning {
		t.Errorf("unexpected fields: %+v", info)
	}
	if info.CreatedAt != "2024-03-10T12:00:00Z" {
		t.Errorf("created_at = %q, want %q", info.CreatedAt, "2024-03-10T12:00:00Z")
	}
}

package runtime

import (
	"sync"
	"testing"

	"github.com/takezoh/agent-roost/state"
)

// spyEventLog captures Append calls for assertion.
type spyEventLog struct {
	mu    sync.Mutex
	calls []spyAppend
}

type spyAppend struct {
	frameID state.FrameID
	line    string
}

func (s *spyEventLog) Append(frameID state.FrameID, line string) error {
	s.mu.Lock()
	s.calls = append(s.calls, spyAppend{frameID, line})
	s.mu.Unlock()
	return nil
}

func (s *spyEventLog) Close(state.FrameID) {}
func (s *spyEventLog) CloseAll()           {}

func TestOscEventLogLine(t *testing.T) {
	cases := []struct {
		source, title, body, want string
	}{
		{"osc9", "Hello agent", "", "[osc9] Hello agent"},
		{"osc99", "", "some body", "[osc99] some body"},
		{"osc777", "Build Done", "task finished", "[osc777] Build Done | task finished"},
	}
	for _, tt := range cases {
		got := oscEventLogLine(tt.source, tt.title, tt.body)
		if got != tt.want {
			t.Errorf("oscEventLogLine(%q,%q,%q) = %q, want %q", tt.source, tt.title, tt.body, got, tt.want)
		}
	}
}

func TestExecuteEffRecordNotificationWritesEventLog(t *testing.T) {
	cases := []struct {
		name        string
		cmd         int
		title, body string
		wantLine    string
	}{
		{"osc9", 9, "Hello agent", "", "[osc9] Hello agent"},
		{"osc99", 99, "", "some body", "[osc99] some body"},
		{"osc777", 777, "Build Done", "task finished", "[osc777] Build Done | task finished"},
	}

	frameID := state.FrameID("frame-1")
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			spy := &spyEventLog{}
			r := New(Config{EventLog: spy})

			r.execute(state.EffRecordNotification{
				FrameID: frameID,
				Cmd:     tt.cmd,
				Title:   tt.title,
				Body:    tt.body,
			})

			spy.mu.Lock()
			calls := spy.calls
			spy.mu.Unlock()

			if len(calls) != 1 {
				t.Fatalf("Append called %d times, want 1", len(calls))
			}
			if calls[0].frameID != frameID {
				t.Errorf("frameID = %q, want %q", calls[0].frameID, frameID)
			}
			if calls[0].line != tt.wantLine {
				t.Errorf("line = %q, want %q", calls[0].line, tt.wantLine)
			}
		})
	}
}

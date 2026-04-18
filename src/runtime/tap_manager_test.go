package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/takezoh/agent-roost/state"
)

func TestReadTapEmitsPaneActivity(t *testing.T) {
	frameID := state.FrameID("f1")
	pane := "%1"
	ch := make(chan []byte, 4)
	ch <- []byte("hello")
	close(ch)

	var events []state.Event
	enqueue := func(e state.Event) { events = append(events, e) }

	readTap(context.Background(), frameID, pane, ch, enqueue)

	var gotActivity bool
	for _, ev := range events {
		if a, ok := ev.(state.EvPaneActivity); ok {
			gotActivity = true
			if a.FrameID != frameID {
				t.Errorf("FrameID = %q, want %q", a.FrameID, frameID)
			}
			if a.PaneTarget != pane {
				t.Errorf("PaneTarget = %q, want %q", a.PaneTarget, pane)
			}
		}
	}
	if !gotActivity {
		t.Error("expected EvPaneActivity event")
	}
}

func TestReadTapDebounceActivity(t *testing.T) {
	frameID := state.FrameID("f1")
	ch := make(chan []byte, 4)
	ch <- []byte("a")
	ch <- []byte("b")
	ch <- []byte("c")
	close(ch)

	var events []state.Event
	enqueue := func(e state.Event) { events = append(events, e) }

	readTap(context.Background(), frameID, "%1", ch, enqueue)

	var activityCount int
	for _, ev := range events {
		if _, ok := ev.(state.EvPaneActivity); ok {
			activityCount++
		}
	}
	// Three rapid messages arrive within 100ms → only 1 activity event expected.
	if activityCount != 1 {
		t.Errorf("activity event count = %d, want 1 (debounced)", activityCount)
	}
}

func TestReadTapEmitsOscEvents(t *testing.T) {
	frameID := state.FrameID("f1")
	ch := make(chan []byte, 4)
	ch <- []byte("\x1b]9;hello world\x07")
	close(ch)

	var events []state.Event
	enqueue := func(e state.Event) { events = append(events, e) }

	readTap(context.Background(), frameID, "%1", ch, enqueue)

	var gotOsc bool
	for _, ev := range events {
		if o, ok := ev.(state.EvPaneOsc); ok {
			gotOsc = true
			if o.Cmd != 9 {
				t.Errorf("Cmd = %d, want 9", o.Cmd)
			}
			if o.Title != "hello world" {
				t.Errorf("Title = %q, want %q", o.Title, "hello world")
			}
		}
	}
	if !gotOsc {
		t.Error("expected EvPaneOsc event")
	}
}

func TestReadTapCancelStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan []byte)

	done := make(chan struct{})
	go func() {
		readTap(ctx, "f1", "%1", ch, func(state.Event) {})
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("readTap did not exit after context cancel")
	}
}

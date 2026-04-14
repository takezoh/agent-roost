package state

import (
	"encoding/json"
	"testing"
	"time"
)

// pushStubState is a driver state used in EffPushDriver tests.
type pushStubState struct {
	DriverStateBase
}

// pushDriverStub emits EffPushDriver from its hook handler.
type pushDriverStub struct{}

func (pushDriverStub) Name() string        { return "pushstub" }
func (pushDriverStub) DisplayName() string { return "pushstub" }
func (pushDriverStub) Status(s DriverState) Status { return StatusIdle }
func (pushDriverStub) NewState(now time.Time) DriverState { return pushStubState{} }
func (pushDriverStub) PrepareLaunch(s DriverState, mode LaunchMode, project, baseCommand string, options LaunchOptions) (LaunchPlan, error) {
	return LaunchPlan{Command: baseCommand, StartDir: project}, nil
}
func (pushDriverStub) Persist(s DriverState) map[string]string { return nil }
func (pushDriverStub) Restore(bag map[string]string, now time.Time) DriverState {
	return pushStubState{}
}
func (pushDriverStub) View(s DriverState) View {
	return View{Card: Card{BorderTitle: Tag{Text: "pushstub"}}}
}
func (pushDriverStub) Step(prev DriverState, ev DriverEvent) (DriverState, []Effect, View) {
	// On any hook, emit EffPushDriver with empty SessionID (to be filled by postProcessEffect).
	if _, ok := ev.(DEvHook); ok {
		return prev, []Effect{EffPushDriver{Command: "stub"}}, View{}
	}
	return prev, nil, View{}
}

func init() {
	if _, exists := registry["pushstub"]; !exists {
		Register(pushDriverStub{})
	}
}

func TestDriverHookEffPushDriverIsResolved(t *testing.T) {
	s := New()
	s.Now = time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	sid := SessionID("sess-push")
	frameID := FrameID("frame-push")
	s.Sessions = map[SessionID]Session{
		sid: {
			ID:      sid,
			Project: "/project",
			Command: "pushstub",
			Driver:  pushStubState{},
			Frames: []SessionFrame{{
				ID:      frameID,
				Project: "/project",
				Command: "pushstub",
				Driver:  pushStubState{},
			}},
		},
	}

	payload, _ := json.Marshal(map[string]string{"hook_event_name": "test"})
	next, effs := Reduce(s, EvDriverEvent{
		ConnID:    1,
		ReqID:     "r",
		Event:     "test",
		Timestamp: time.Now(),
		SenderID:  frameID,
		Payload:   json.RawMessage(payload),
	})
	_ = next

	// EffPushDriver should have been resolved into EffSpawnTmuxWindow.
	spawn, ok := findEff[EffSpawnTmuxWindow](effs)
	if !ok {
		t.Fatal("expected EffSpawnTmuxWindow from resolved EffPushDriver")
	}
	// Project should fall back to the parent session's project,
	// since the driver's EffPushDriver carries no project.
	if spawn.Project != "/project" {
		t.Errorf("spawn.Project = %q, want /project", spawn.Project)
	}
	// Session should now have 2 frames.
	sess := next.Sessions[sid]
	if len(sess.Frames) != 2 {
		t.Errorf("frame count = %d, want 2", len(sess.Frames))
	}
	if sess.Frames[1].Project != "/project" {
		t.Errorf("new frame project = %q, want /project", sess.Frames[1].Project)
	}
}

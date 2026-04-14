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

// bogusSessionDriverStub emits EffPushDriver with a non-existent SessionID.
type bogusSessionDriverStub struct{}

func (bogusSessionDriverStub) Name() string        { return "bogussessionstub" }
func (bogusSessionDriverStub) DisplayName() string { return "bogussessionstub" }
func (bogusSessionDriverStub) Status(s DriverState) Status { return StatusIdle }
func (bogusSessionDriverStub) NewState(now time.Time) DriverState { return pushStubState{} }
func (bogusSessionDriverStub) PrepareLaunch(s DriverState, mode LaunchMode, project, baseCommand string, options LaunchOptions) (LaunchPlan, error) {
	return LaunchPlan{Command: baseCommand, StartDir: project}, nil
}
func (bogusSessionDriverStub) Persist(s DriverState) map[string]string { return nil }
func (bogusSessionDriverStub) Restore(bag map[string]string, now time.Time) DriverState {
	return pushStubState{}
}
func (bogusSessionDriverStub) View(s DriverState) View {
	return View{Card: Card{BorderTitle: Tag{Text: "bogussessionstub"}}}
}
func (bogusSessionDriverStub) Step(prev DriverState, ev DriverEvent) (DriverState, []Effect, View) {
	if _, ok := ev.(DEvHook); ok {
		return prev, []Effect{EffPushDriver{SessionID: "does-not-exist", Command: "stub"}}, View{}
	}
	return prev, nil, View{}
}

func init() {
	if _, exists := registry["bogussessionstub"]; !exists {
		Register(bogusSessionDriverStub{})
	}
}

func TestDriverHookEffPushDriverBogusSessionIDDropped(t *testing.T) {
	s := New()
	s.Now = time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	sid := SessionID("sess-bogus")
	frameID := FrameID("frame-bogus")
	s.Sessions = map[SessionID]Session{
		sid: {
			ID:      sid,
			Project: "/project",
			Command: "bogussessionstub",
			Driver:  pushStubState{},
			Frames: []SessionFrame{{
				ID:      frameID,
				Project: "/project",
				Command: "bogussessionstub",
				Driver:  pushStubState{},
			}},
		},
	}

	payload, _ := json.Marshal(map[string]string{"hook_event_name": "test"})
	_, effs := Reduce(s, EvDriverEvent{
		ConnID:    1,
		ReqID:     "r",
		Event:     "test",
		Timestamp: time.Now(),
		SenderID:  frameID,
		Payload:   json.RawMessage(payload),
	})

	// EffPushDriver with bogus SessionID should be dropped — no EffSpawnTmuxWindow.
	if _, ok := findEff[EffSpawnTmuxWindow](effs); ok {
		t.Error("expected EffSpawnTmuxWindow to be absent (bogus SessionID should be dropped)")
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

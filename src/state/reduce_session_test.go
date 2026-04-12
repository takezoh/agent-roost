package state

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// === Test driver registration ===
//
// reduce_session_test.go is the only place state pkg tests need a
// real Driver. We register a tiny stub here in init() so we can drive
// reducers without importing state/driver (which would create an
// import cycle).

type stubJobInput struct{}

func (stubJobInput) JobKind() string { return "stub" }

type stubDriverState struct {
	DriverStateBase
	calls  []string
	status Status
}

type stubDriver struct{}

func (stubDriver) Name() string                                             { return "stub" }
func (stubDriver) DisplayName() string                                      { return "stub" }
func (stubDriver) Status(s DriverState) Status                              { return s.(stubDriverState).status }
func (stubDriver) NewState(now time.Time) DriverState                       { return stubDriverState{} }
func (stubDriver) SpawnCommand(s DriverState, baseCommand string) string    { return baseCommand }
func (stubDriver) Persist(s DriverState) map[string]string                  { return nil }
func (stubDriver) Restore(bag map[string]string, now time.Time) DriverState { return stubDriverState{} }
func (stubDriver) View(s DriverState) View                                  { return View{} }
func (stubDriver) Step(prev DriverState, ev DriverEvent) (DriverState, []Effect, View) {
	return prev, nil, View{}
}

type plannerDriver struct{ stubDriver }

func (plannerDriver) Name() string { return "planner" }
func (plannerDriver) PrepareCreate(s DriverState, sessionID SessionID, project, command string) (DriverState, CreatePlan, error) {
	return s, CreatePlan{
		Launch:   CreateLaunch{Command: "planner --prepared", StartDir: project},
		SetupJob: stubJobInput{},
	}, nil
}
func (plannerDriver) CompleteCreate(s DriverState, command string, result any, err error) (DriverState, CreateLaunch, error) {
	if err != nil {
		return s, CreateLaunch{}, err
	}
	return s, CreateLaunch{Command: "planner --prepared", StartDir: "/prepared"}, nil
}
func (plannerDriver) ManagedWorktreePath(s DriverState) string {
	return "/repo/.roost/worktrees/planner"
}

type fallbackDriver struct{ stubDriver }

func (fallbackDriver) Name() string { return "" }

func init() {
	if _, exists := registry[""]; !exists {
		Register(fallbackDriver{})
	}
	if _, exists := registry["stub"]; !exists {
		Register(stubDriver{})
	}
	if _, exists := registry["planner"]; !exists {
		Register(plannerDriver{})
	}
}

// === Helpers ===

func mustOK(t *testing.T, effs []Effect) {
	t.Helper()
	for _, e := range effs {
		if _, ok := e.(EffSendError); ok {
			t.Fatalf("unexpected error effect: %+v", e)
		}
	}
}

func findEff[T Effect](effs []Effect) (T, bool) {
	var zero T
	for _, e := range effs {
		if v, ok := e.(T); ok {
			return v, true
		}
	}
	return zero, false
}

func countEff[T Effect](effs []Effect) int {
	n := 0
	for _, e := range effs {
		if _, ok := e.(T); ok {
			n++
		}
	}
	return n
}

func mustPayload(fields map[string]string) json.RawMessage {
	b, _ := json.Marshal(fields)
	return json.RawMessage(b)
}

// === reduceCreateSession ===

func TestCreateSessionMissingProject(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvEvent{ConnID: 1, ReqID: "r", Event: "create-session"})
	if _, ok := findEff[EffSendError](effs); !ok {
		t.Error("expected EffSendError")
	}
}

func TestCreateSessionAllocatesAndSpawns(t *testing.T) {
	s := New()
	s.Now = time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	next, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "create-session",
		Payload: mustPayload(map[string]string{"project": "/foo", "command": "stub"}),
	})
	if len(next.Sessions) != 1 {
		t.Fatalf("session count = %d, want 1", len(next.Sessions))
	}
	var sess Session
	for _, v := range next.Sessions {
		sess = v
	}
	if sess.Project != "/foo" || sess.Command != "stub" {
		t.Errorf("session = %+v", sess)
	}
	spawn, ok := findEff[EffSpawnTmuxWindow](effs)
	if !ok {
		t.Fatal("expected EffSpawnTmuxWindow")
	}
	if spawn.SessionID != sess.ID || spawn.Project != "/foo" || spawn.Command != "stub" {
		t.Errorf("spawn = %+v", spawn)
	}
	if spawn.ReplyConn != 1 || spawn.ReplyReqID != "r" {
		t.Error("spawn missing reply context")
	}
	if spawn.Env["ROOST_SESSION_ID"] != string(sess.ID) {
		t.Errorf("env ROOST_SESSION_ID = %q", spawn.Env["ROOST_SESSION_ID"])
	}
}

func TestCreateSessionDefaultCommand(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "create-session",
		Payload: mustPayload(map[string]string{"project": "/foo"}),
	})
	if _, ok := findEff[EffSpawnTmuxWindow](effs); !ok {
		t.Error("expected EffSpawnTmuxWindow with default command")
	}
}

func TestCreateSessionUnknownCommandFallsBackToFallback(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "create-session",
		Payload: mustPayload(map[string]string{"project": "/foo", "command": "nonexistent"}),
	})
	if _, ok := findEff[EffSpawnTmuxWindow](effs); !ok {
		t.Error("expected fallback driver to allow spawn")
	}
}

func TestCreateSessionPlannerDefersSpawnUntilJobResult(t *testing.T) {
	s := New()
	s.Now = time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	next, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "create-session",
		Payload: mustPayload(map[string]string{"project": "/foo", "command": "planner"}),
	})
	if len(next.Sessions) != 0 {
		t.Fatalf("session count = %d, want 0 before setup result", len(next.Sessions))
	}
	if len(next.PendingCreates) != 1 {
		t.Fatalf("pending creates = %d, want 1", len(next.PendingCreates))
	}
	job, ok := findEff[EffStartJob](effs)
	if !ok {
		t.Fatal("expected EffStartJob")
	}
	if _, ok := job.Input.(stubJobInput); !ok {
		t.Fatalf("job input = %T, want stubJobInput", job.Input)
	}

	after, effs := Reduce(next, EvJobResult{JobID: job.JobID, Result: "ok"})
	if len(after.PendingCreates) != 0 {
		t.Fatalf("pending creates = %d, want 0 after completion", len(after.PendingCreates))
	}
	if len(after.Sessions) != 1 {
		t.Fatalf("session count = %d, want 1 after completion", len(after.Sessions))
	}
	spawn, ok := findEff[EffSpawnTmuxWindow](effs)
	if !ok {
		t.Fatal("expected EffSpawnTmuxWindow after setup completion")
	}
	if spawn.Command != "planner --prepared" || spawn.StartDir != "/prepared" {
		t.Fatalf("spawn = %+v", spawn)
	}
}

func TestCreateSessionPlannerFailureRepliesError(t *testing.T) {
	s := New()
	next, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "create-session",
		Payload: mustPayload(map[string]string{"project": "/foo", "command": "planner"}),
	})
	job, ok := findEff[EffStartJob](effs)
	if !ok {
		t.Fatal("expected EffStartJob")
	}
	after, effs := Reduce(next, EvJobResult{JobID: job.JobID, Err: errors.New("boom")})
	if len(after.Sessions) != 0 {
		t.Fatalf("session count = %d, want 0", len(after.Sessions))
	}
	if _, ok := findEff[EffSendError](effs); !ok {
		t.Fatal("expected EffSendError")
	}
}

// === reduceTmuxPaneSpawned ===

func TestTmuxSpawnedRegistersWindowAndActivates(t *testing.T) {
	s := New()
	s.Now = time.Now()
	id := SessionID("abc")
	s.Sessions[id] = Session{ID: id, Project: "/foo", Command: "stub", Driver: stubDriverState{}}

	next, effs := Reduce(s, EvTmuxPaneSpawned{
		SessionID:  id,
		PaneTarget: "%1",
		ReplyConn:  1,
		ReplyReqID: "r",
	})
	if next.ActiveSession != id {
		t.Errorf("ActiveSession = %q, want %q", next.ActiveSession, id)
	}
	if _, ok := findEff[EffRegisterPane](effs); !ok {
		t.Error("expected EffRegisterPane")
	}
	if _, ok := findEff[EffActivateSession](effs); !ok {
		t.Error("expected EffActivateSession")
	}
	if _, ok := findEff[EffPersistSnapshot](effs); !ok {
		t.Error("expected EffPersistSnapshot")
	}
	if _, ok := findEff[EffBroadcastSessionsChanged](effs); !ok {
		t.Error("expected EffBroadcastSessionsChanged")
	}
	if _, ok := findEff[EffSendResponse](effs); !ok {
		t.Error("expected EffSendResponse")
	}
}

func TestTmuxSpawnedUnknownSessionDropsSilently(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvTmuxPaneSpawned{
		SessionID: "ghost", PaneTarget: "%1", ReplyConn: 1, ReplyReqID: "r",
	})
	if len(effs) != 0 {
		t.Errorf("expected no effects, got %d", len(effs))
	}
}

func TestTmuxSpawnFailedEvictsAndReplies(t *testing.T) {
	s := New()
	id := SessionID("abc")
	s.Sessions[id] = Session{ID: id}
	next, effs := Reduce(s, EvTmuxSpawnFailed{
		SessionID: id, Err: "boom", ReplyConn: 1, ReplyReqID: "r",
	})
	if _, ok := next.Sessions[id]; ok {
		t.Error("session should be evicted")
	}
	if _, ok := findEff[EffSendError](effs); !ok {
		t.Error("expected EffSendError")
	}
}

func TestTmuxSpawnFailedRemovesManagedWorktree(t *testing.T) {
	s := New()
	id := SessionID("abc")
	s.Sessions[id] = Session{ID: id, Command: "planner", Driver: stubDriverState{}}
	_, effs := Reduce(s, EvTmuxSpawnFailed{
		SessionID: id, Err: "boom", ReplyConn: 1, ReplyReqID: "r",
	})
	if _, ok := findEff[EffRemoveManagedWorktree](effs); !ok {
		t.Fatal("expected EffRemoveManagedWorktree")
	}
}

func TestTmuxSpawnFailedNoManagedWorktreeForStub(t *testing.T) {
	s := New()
	id := SessionID("abc")
	s.Sessions[id] = Session{ID: id, Command: "stub", Driver: stubDriverState{}}
	_, effs := Reduce(s, EvTmuxSpawnFailed{
		SessionID: id, Err: "boom", ReplyConn: 1, ReplyReqID: "r",
	})
	if _, ok := findEff[EffRemoveManagedWorktree](effs); ok {
		t.Fatal("did not expect EffRemoveManagedWorktree")
	}
}

// === reduceStopSession ===

func TestStopSessionRemovesAndKills(t *testing.T) {
	s := New()
	id := SessionID("abc")
	s.Sessions[id] = Session{ID: id, Command: "stub", Driver: stubDriverState{}}
	s.ActiveSession = id
	next, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "stop-session",
		Payload: mustPayload(map[string]string{"session_id": string(id)}),
	})
	if _, ok := next.Sessions[id]; ok {
		t.Error("session should be removed")
	}
	if next.ActiveSession != "" {
		t.Errorf("ActiveSession = %q, want empty", next.ActiveSession)
	}
	if _, ok := findEff[EffKillSessionWindow](effs); !ok {
		t.Error("expected EffKillSessionWindow")
	}
	if _, ok := findEff[EffUnregisterPane](effs); !ok {
		t.Error("expected EffUnregisterPane")
	}
	mustOK(t, effs)
}

func TestStopSessionUnknownReturnsError(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "stop-session",
		Payload: mustPayload(map[string]string{"session_id": "ghost"}),
	})
	if _, ok := findEff[EffSendError](effs); !ok {
		t.Error("expected EffSendError")
	}
}

func TestStopActiveSessionEmitsDeactivate(t *testing.T) {
	s := New()
	id := SessionID("abc")
	s.Sessions[id] = Session{ID: id, Command: "stub", Driver: stubDriverState{}}
	s.ActiveSession = id
	_, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "stop-session",
		Payload: mustPayload(map[string]string{"session_id": string(id)}),
	})
	if _, ok := findEff[EffDeactivateSession](effs); !ok {
		t.Error("expected EffDeactivateSession when stopping active session")
	}
}

func TestStopInactiveSessionNoDeactivate(t *testing.T) {
	s := New()
	id := SessionID("abc")
	other := SessionID("other")
	s.Sessions[id] = Session{ID: id, Command: "stub", Driver: stubDriverState{}}
	s.ActiveSession = other
	_, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "stop-session",
		Payload: mustPayload(map[string]string{"session_id": string(id)}),
	})
	if _, ok := findEff[EffDeactivateSession](effs); ok {
		t.Error("should not emit EffDeactivateSession for inactive session")
	}
}

// === reducePreviewSession / reduceSwitchSession ===

func TestPreviewSessionActivatesAndBroadcasts(t *testing.T) {
	s := New()
	id := SessionID("abc")
	s.Sessions[id] = Session{ID: id, Command: "stub", Driver: stubDriverState{}}
	next, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "preview-session",
		Payload: mustPayload(map[string]string{"session_id": string(id)}),
	})
	if next.ActiveSession != id {
		t.Errorf("ActiveSession = %q, want %q", next.ActiveSession, id)
	}
	if eff, ok := findEff[EffActivateSession](effs); !ok {
		t.Error("expected EffActivateSession")
	} else if eff.Reason != EventPreviewSession {
		t.Errorf("EffActivateSession.Reason = %q, want %q", eff.Reason, EventPreviewSession)
	}
	mustOK(t, effs)
}

func TestPreviewSessionUnknownErrors(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "preview-session",
		Payload: mustPayload(map[string]string{"session_id": "ghost"}),
	})
	if _, ok := findEff[EffSendError](effs); !ok {
		t.Error("expected EffSendError")
	}
}

func TestSwitchSessionAlsoSelectsPane(t *testing.T) {
	s := New()
	id := SessionID("abc")
	s.Sessions[id] = Session{ID: id, Command: "stub", Driver: stubDriverState{}}
	_, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "switch-session",
		Payload: mustPayload(map[string]string{"session_id": string(id)}),
	})
	if eff, ok := findEff[EffActivateSession](effs); !ok {
		t.Error("expected EffActivateSession")
	} else if eff.Reason != EventSwitchSession {
		t.Errorf("EffActivateSession.Reason = %q, want %q", eff.Reason, EventSwitchSession)
	}
	if _, ok := findEff[EffSelectPane](effs); !ok {
		t.Error("expected EffSelectPane")
	}
	mustOK(t, effs)
}

// === reducePreviewProject ===

func TestPreviewProjectDeactivatesActive(t *testing.T) {
	s := New()
	s.ActiveSession = "abc"
	next, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "preview-project",
		Payload: mustPayload(map[string]string{"project": "/foo"}),
	})
	if next.ActiveSession != "" {
		t.Errorf("ActiveSession = %q, want empty", next.ActiveSession)
	}
	if _, ok := findEff[EffDeactivateSession](effs); !ok {
		t.Error("expected EffDeactivateSession to swap back")
	}
	if _, ok := findEff[EffBroadcastEvent](effs); !ok {
		t.Error("expected EffBroadcastEvent")
	}
}

func TestPreviewProjectNoActiveStillBroadcasts(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "preview-project",
		Payload: mustPayload(map[string]string{"project": "/foo"}),
	})
	if _, ok := findEff[EffBroadcastEvent](effs); !ok {
		t.Error("expected EffBroadcastEvent")
	}
}

// === reduceListSessions ===

func TestListSessionsResponds(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvEvent{ConnID: 1, ReqID: "r", Event: "list-sessions"})
	if _, ok := findEff[EffSendResponse](effs); !ok {
		t.Error("expected EffSendResponse")
	}
}

// === reduceFocusPane ===

func TestFocusPaneSelectsAndBroadcasts(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "focus-pane",
		Payload: mustPayload(map[string]string{"pane": "0.1"}),
	})
	if _, ok := findEff[EffSelectPane](effs); !ok {
		t.Error("expected EffSelectPane")
	}
	if _, ok := findEff[EffBroadcastEvent](effs); !ok {
		t.Error("expected EffBroadcastEvent")
	}
}

func TestFocusPaneEmptyErrors(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvEvent{ConnID: 1, ReqID: "r", Event: "focus-pane"})
	if _, ok := findEff[EffSendError](effs); !ok {
		t.Error("expected EffSendError")
	}
}

// === reduceLaunchTool ===

func TestLaunchToolDisplaysPopup(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "launch-tool",
		Payload: mustPayload(map[string]string{"tool": "new-session"}),
	})
	if _, ok := findEff[EffDisplayPopup](effs); !ok {
		t.Error("expected EffDisplayPopup")
	}
}

func TestLaunchToolEmptyErrors(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvEvent{ConnID: 1, ReqID: "r", Event: "launch-tool"})
	if _, ok := findEff[EffSendError](effs); !ok {
		t.Error("expected EffSendError")
	}
}

// === reduceShutdown / reduceDetach ===

func TestShutdownKillsSession(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvEvent{ConnID: 1, ReqID: "r", Event: "shutdown"})
	if _, ok := findEff[EffKillSession](effs); !ok {
		t.Error("expected EffKillSession")
	}
	if _, ok := findEff[EffSendResponseSync](effs); !ok {
		t.Error("expected EffSendResponseSync")
	}
	if _, ok := findEff[EffSendResponse](effs); ok {
		t.Error("did not expect EffSendResponse")
	}
	if _, ok := findEff[EffDetachClient](effs); ok {
		t.Error("did not expect EffDetachClient")
	}
}

func TestDetach(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvEvent{ConnID: 1, ReqID: "r", Event: "detach"})
	if _, ok := findEff[EffDetachClient](effs); !ok {
		t.Error("expected EffDetachClient")
	}
}

// === reduceTmuxWindowVanished ===

func TestTmuxWindowVanishedEvicts(t *testing.T) {
	s := New()
	id := SessionID("abc")
	s.Sessions[id] = Session{ID: id, Command: "stub", Driver: stubDriverState{}}
	s.ActiveSession = id
	next, effs := Reduce(s, EvTmuxWindowVanished{SessionID: id})
	if _, ok := next.Sessions[id]; ok {
		t.Error("session should be evicted")
	}
	if next.ActiveSession != "" {
		t.Errorf("ActiveSession not cleared: %q", next.ActiveSession)
	}
	if _, ok := findEff[EffBroadcastSessionsChanged](effs); !ok {
		t.Error("expected broadcast")
	}
	if _, ok := findEff[EffUnregisterPane](effs); !ok {
		t.Error("expected EffUnregisterPane")
	}
}

// === reduceConnOpened / reduceConnClosed ===

func TestConnOpenedRecordsHighWaterMark(t *testing.T) {
	s := New()
	next, _ := Reduce(s, EvConnOpened{ConnID: 7})
	if next.NextConnID != 7 {
		t.Errorf("NextConnID = %d, want 7", next.NextConnID)
	}
}

func TestConnClosedRemovesSubscriber(t *testing.T) {
	s := New()
	s.Subscribers[5] = Subscriber{ConnID: 5}
	next, _ := Reduce(s, EvConnClosed{ConnID: 5})
	if _, ok := next.Subscribers[5]; ok {
		t.Error("subscriber should be removed")
	}
}

func TestSubscribeAddsAndBroadcasts(t *testing.T) {
	s := New()
	next, effs := Reduce(s, EvCmdSubscribe{ConnID: 5, ReqID: "r", Filters: []string{"sessions-changed"}})
	if _, ok := next.Subscribers[5]; !ok {
		t.Error("subscriber not registered")
	}
	if _, ok := findEff[EffBroadcastSessionsChanged](effs); !ok {
		t.Error("expected initial broadcast")
	}
}

func TestUnsubscribeRemoves(t *testing.T) {
	s := New()
	s.Subscribers[5] = Subscriber{ConnID: 5}
	next, _ := Reduce(s, EvCmdUnsubscribe{ConnID: 5, ReqID: "r"})
	if _, ok := next.Subscribers[5]; ok {
		t.Error("subscriber should be removed")
	}
}

// === reducePaneDied ===

func TestPaneDiedEmitsRespawn(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvPaneDied{Pane: "{sessionName}:0.1"})
	respawn, ok := findEff[EffRespawnPane](effs)
	if !ok {
		t.Fatal("expected EffRespawnPane")
	}
	if respawn.Pane != "{sessionName}:0.1" {
		t.Errorf("pane = %q", respawn.Pane)
	}
}

func TestPaneDiedUnknownPaneIsNoop(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvPaneDied{Pane: "garbage"})
	if len(effs) != 0 {
		t.Errorf("expected 0 effects, got %d", len(effs))
	}
}

func TestPaneDiedEvictsSessionByOwnerID(t *testing.T) {
	s := New()
	s.Sessions = map[SessionID]Session{
		"s1": {ID: "s1", Command: "stub", Driver: stubDriverState{}},
	}
	s.ActiveSession = "s1"

	next, effs := Reduce(s, EvPaneDied{
		Pane:           "{sessionName}:0.0",
		OwnerSessionID: "s1",
	})
	if _, ok := next.Sessions["s1"]; ok {
		t.Fatal("session should be deleted")
	}
	if next.ActiveSession != "" {
		t.Errorf("ActiveSession = %q, want empty", next.ActiveSession)
	}
	if _, ok := findEff[EffKillSessionWindow](effs); !ok {
		t.Error("expected EffKillSessionWindow")
	}
	if _, ok := findEff[EffBroadcastSessionsChanged](effs); !ok {
		t.Error("expected EffBroadcastSessionsChanged")
	}
	if _, ok := findEff[EffRespawnPane](effs); ok {
		t.Error("should not respawn pane 0.0 directly after eviction")
	}
}

func TestPaneDiedFallbackViaActiveSession(t *testing.T) {
	s := New()
	s.Sessions = map[SessionID]Session{
		"s1": {ID: "s1", Command: "stub", Driver: stubDriverState{}},
	}
	s.ActiveSession = "s1"

	next, effs := Reduce(s, EvPaneDied{
		Pane:           "{sessionName}:0.0",
		OwnerSessionID: "", // runtime couldn't identify owner
	})
	if _, ok := next.Sessions["s1"]; ok {
		t.Fatal("session should be deleted via ActiveSession fallback")
	}
	if _, ok := findEff[EffKillSessionWindow](effs); !ok {
		t.Error("expected EffKillSessionWindow")
	}
	if _, ok := findEff[EffRespawnPane](effs); ok {
		t.Error("should not respawn pane 0.0 directly after fallback eviction")
	}
}

func TestPaneDiedNoActiveRespawnsMainTUI(t *testing.T) {
	s := New()
	s.Sessions = map[SessionID]Session{
		"s1": {ID: "s1", Command: "stub", Driver: stubDriverState{}},
	}

	_, effs := Reduce(s, EvPaneDied{
		Pane:           "{sessionName}:0.0",
		OwnerSessionID: "",
	})
	respawn, ok := findEff[EffRespawnPane](effs)
	if !ok {
		t.Fatal("expected EffRespawnPane for main TUI")
	}
	if respawn.Pane != "{sessionName}:0.0" {
		t.Errorf("pane = %q, want {sessionName}:0.0", respawn.Pane)
	}
	if respawn.Cmd != "{roostExe} --tui main" {
		t.Errorf("cmd = %q, want main TUI command", respawn.Cmd)
	}
}

// === reduceJobResult ===

func TestJobResultUnknownDropsSilently(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvJobResult{JobID: 99})
	if len(effs) != 0 {
		t.Errorf("expected 0 effects, got %d", len(effs))
	}
}

func TestJobResultRoutesToDriver(t *testing.T) {
	s := New()
	id := SessionID("abc")
	s.Sessions[id] = Session{ID: id, Command: "stub", Driver: stubDriverState{}}
	s.Jobs[1] = JobMeta{SessionID: id}
	next, effs := Reduce(s, EvJobResult{JobID: 1, Result: "irrelevant"})
	if _, ok := next.Jobs[1]; ok {
		t.Error("job should be removed")
	}
	if countEff[EffPersistSnapshot](effs) != 1 {
		t.Errorf("persist count = %d", countEff[EffPersistSnapshot](effs))
	}
}

func TestJobResultRoutesToConnector(t *testing.T) {
	orig := connectorRegistry
	connectorRegistry = map[string]Connector{}
	defer func() { connectorRegistry = orig }()

	RegisterConnector(stubConnector{name: "test"})

	s := New()
	s.ConnectorsReady = true
	s.Connectors["test"] = stubConnectorState{}
	s.Jobs[1] = JobMeta{Connector: "test"}

	next, effs := Reduce(s, EvJobResult{JobID: 1, Result: "data"})
	if _, ok := next.Jobs[1]; ok {
		t.Error("job should be removed")
	}
	if countEff[EffBroadcastSessionsChanged](effs) != 1 {
		t.Errorf("broadcast count = %d, want 1", countEff[EffBroadcastSessionsChanged](effs))
	}
	cs := next.Connectors["test"].(stubConnectorState)
	if cs.Val != 1 {
		t.Errorf("Val = %d, want 1", cs.Val)
	}
}

// === reduceFileChanged ===

func TestFileChangedRoutes(t *testing.T) {
	s := New()
	id := SessionID("abc")
	s.Sessions[id] = Session{ID: id, Command: "stub", Driver: stubDriverState{}}
	_, effs := Reduce(s, EvFileChanged{SessionID: id, Path: "/x"})
	if len(effs) != 0 {
		t.Errorf("expected 0 effects from no-op driver, got %d", len(effs))
	}
}

func TestFileChangedUnknownSession(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvFileChanged{SessionID: "ghost", Path: "/x"})
	if len(effs) != 0 {
		t.Errorf("expected 0 effects, got %d", len(effs))
	}
}

// === reduceHook ===

func TestReduceHookMissingSenderID(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvDriverEvent{ConnID: 1, ReqID: "r", Event: "custom-hook"})
	if _, ok := findEff[EffSendError](effs); !ok {
		t.Error("expected EffSendError")
	}
}

func TestReduceHookUnknownSession(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvDriverEvent{ConnID: 1, ReqID: "r", Event: "custom-hook", SenderID: "ghost"})
	if _, ok := findEff[EffSendError](effs); !ok {
		t.Error("expected EffSendError")
	}
}

func TestReduceHookRoutes(t *testing.T) {
	s := New()
	id := SessionID("abc")
	s.Sessions[id] = Session{ID: id, Command: "stub", Driver: stubDriverState{}}
	_, effs := Reduce(s, EvDriverEvent{
		ConnID: 1, ReqID: "r", SenderID: id, Event: "session-start",
		Payload: json.RawMessage(`{}`),
	})
	if _, ok := findEff[EffSendResponse](effs); !ok {
		t.Error("expected EffSendResponse")
	}
}

func TestReduceHookInjectsRoostSessionID(t *testing.T) {
	s := New()
	id := SessionID("roost-xyz")
	s.Sessions[id] = Session{ID: id, Command: "stub", Driver: stubDriverState{}}
	_, effs := Reduce(s, EvDriverEvent{
		ConnID: 1, ReqID: "r", SenderID: id, Event: "test",
		Payload: json.RawMessage(`{}`),
	})
	if _, ok := findEff[EffSendError](effs); ok {
		t.Error("unexpected error from hook with empty payload")
	}
}

// === postProcessEffect ===

func TestPostProcessAssignsJobID(t *testing.T) {
	s := New()
	s.Now = time.Now()
	patched, next := postProcessEffect(s, "abc", EffStartJob{Input: stubJobInput{}})
	job := patched.(EffStartJob)
	if job.JobID == 0 {
		t.Error("JobID should be assigned")
	}
	if next.NextJobID != job.JobID {
		t.Errorf("NextJobID = %d, want %d", next.NextJobID, job.JobID)
	}
	meta, ok := next.Jobs[job.JobID]
	if !ok {
		t.Fatal("JobMeta not registered")
	}
	if meta.SessionID != "abc" {
		t.Errorf("meta.SessionID = %q, want abc", meta.SessionID)
	}
}

func TestPostProcessFillsSessionID(t *testing.T) {
	s := New()
	patched, _ := postProcessEffect(s, "abc", EffEventLogAppend{Line: "x"})
	eff := patched.(EffEventLogAppend)
	if eff.SessionID != "abc" {
		t.Errorf("SessionID = %q, want abc", eff.SessionID)
	}
}

func TestPostProcessLeavesSessionIDIfSet(t *testing.T) {
	s := New()
	patched, _ := postProcessEffect(s, "abc", EffWatchFile{SessionID: "preset", Path: "/x"})
	if patched.(EffWatchFile).SessionID != "preset" {
		t.Error("preset SessionID overwritten")
	}
}

// === DefaultCommand ===

func TestReduceCreateSession_DefaultCommand(t *testing.T) {
	s := New()
	s.DefaultCommand = "gemini"
	s, _ = Reduce(s, EvEvent{
		Event:   "create-session",
		Payload: mustPayload(map[string]string{"project": "test"}),
		ConnID:  1, ReqID: "r1",
	})
	for _, sess := range s.Sessions {
		if sess.Command != "gemini" {
			t.Errorf("Command = %q, want gemini", sess.Command)
		}
		return
	}
	t.Fatal("no session created")
}

func TestReduceCreateSession_FallbackToShell(t *testing.T) {
	s := New()
	s, _ = Reduce(s, EvEvent{
		Event:   "create-session",
		Payload: mustPayload(map[string]string{"project": "test"}),
		ConnID:  1, ReqID: "r1",
	})
	for _, sess := range s.Sessions {
		if sess.Command != "shell" {
			t.Errorf("Command = %q, want shell", sess.Command)
		}
		return
	}
	t.Fatal("no session created")
}

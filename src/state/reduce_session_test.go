package state

import (
	"testing"
	"time"
)

// === Test driver registration ===
//
// reduce_session_test.go is the only place state pkg tests need a
// real Driver. We register a tiny stub here in init() so we can drive
// reducers without importing state/driver (which would create an
// import cycle).

type stubDriverState struct {
	DriverStateBase
	calls []string
}

type stubDriver struct{}

func (stubDriver) Name() string                                                  { return "stub" }
func (stubDriver) DisplayName() string                                           { return "stub" }
func (stubDriver) NewState(now time.Time) DriverState                            { return stubDriverState{} }
func (stubDriver) SpawnCommand(s DriverState, baseCommand string) string         { return baseCommand }
func (stubDriver) Persist(s DriverState) map[string]string                       { return nil }
func (stubDriver) Restore(bag map[string]string, now time.Time) DriverState      { return stubDriverState{} }
func (stubDriver) View(s DriverState) View                                       { return View{} }
func (stubDriver) Step(prev DriverState, ev DriverEvent) (DriverState, []Effect, View) {
	return prev, nil, View{}
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

// === reduceCreateSession ===

func TestCreateSessionMissingProject(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvCmdCreateSession{ConnID: 1, ReqID: "r"})
	if _, ok := findEff[EffSendError](effs); !ok {
		t.Error("expected EffSendError")
	}
}

func TestCreateSessionAllocatesAndSpawns(t *testing.T) {
	s := New()
	s.Now = time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	next, effs := Reduce(s, EvCmdCreateSession{
		ConnID: 1, ReqID: "r", Project: "/foo", Command: "stub",
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
	if sess.WindowID != "" {
		t.Errorf("WindowID should be empty until spawn callback, got %q", sess.WindowID)
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
	// command="" defaults to "claude" inside reduceCreateSession.
	// We don't have a real claude driver registered in state pkg
	// tests (it lives in state/driver), so the lookup falls back
	// to the registered fallback driver "" — which works because
	// the command string "claude" still routes through the registry
	// and falls back when not found.
	s := New()
	_, effs := Reduce(s, EvCmdCreateSession{
		ConnID: 1, ReqID: "r", Project: "/foo", // no Command → defaults to "claude"
	})
	// The fallback driver handles unknown commands so spawn is emitted.
	if _, ok := findEff[EffSpawnTmuxWindow](effs); !ok {
		t.Error("expected EffSpawnTmuxWindow with default command")
	}
}

func TestCreateSessionUnknownCommandFallsBackToFallback(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvCmdCreateSession{
		ConnID: 1, ReqID: "r", Project: "/foo", Command: "nonexistent",
	})
	// Falls back to "" registered driver, so spawn is emitted.
	if _, ok := findEff[EffSpawnTmuxWindow](effs); !ok {
		t.Error("expected fallback driver to allow spawn")
	}
}

// === reduceTmuxWindowSpawned ===

func TestTmuxSpawnedFillsWindow(t *testing.T) {
	s := New()
	s.Now = time.Now()
	id := SessionID("abc")
	s.Sessions[id] = Session{ID: id, Project: "/foo", Command: "stub", Driver: stubDriverState{}}

	next, effs := Reduce(s, EvTmuxWindowSpawned{
		SessionID:   id,
		WindowID:    "@5",
		PaneID: "%10",
		ReplyConn:   1,
		ReplyReqID:  "r",
	})
	sess := next.Sessions[id]
	if sess.WindowID != "@5" || sess.PaneID != "%10" {
		t.Errorf("session not updated: %+v", sess)
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
	_, effs := Reduce(s, EvTmuxWindowSpawned{
		SessionID: "ghost", WindowID: "@5", ReplyConn: 1, ReplyReqID: "r",
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

// === reduceStopSession ===

func TestStopSessionRemovesAndKills(t *testing.T) {
	s := New()
	id := SessionID("abc")
	s.Sessions[id] = Session{ID: id, WindowID: "@5", Driver: stubDriverState{}}
	s.Active = "@5"
	next, effs := Reduce(s, EvCmdStopSession{ConnID: 1, ReqID: "r", SessionID: id})
	if _, ok := next.Sessions[id]; ok {
		t.Error("session should be removed")
	}
	if next.Active != "" {
		t.Errorf("active = %q, want empty", next.Active)
	}
	if _, ok := findEff[EffKillTmuxWindow](effs); !ok {
		t.Error("expected EffKillTmuxWindow")
	}
	mustOK(t, effs)
}

func TestStopSessionUnknownReturnsError(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvCmdStopSession{ConnID: 1, ReqID: "r", SessionID: "ghost"})
	if _, ok := findEff[EffSendError](effs); !ok {
		t.Error("expected EffSendError")
	}
}

// === reducePreviewSession / reduceSwitchSession ===

func TestPreviewSessionSwapsAndUpdatesActive(t *testing.T) {
	s := New()
	id := SessionID("abc")
	s.Sessions[id] = Session{ID: id, WindowID: "@5", Driver: stubDriverState{}}
	next, effs := Reduce(s, EvCmdPreviewSession{ConnID: 1, ReqID: "r", SessionID: id})
	if next.Active != "@5" {
		t.Errorf("active = %q, want @5", next.Active)
	}
	if _, ok := findEff[EffSwapPane](effs); !ok {
		t.Error("expected EffSwapPane")
	}
	if _, ok := findEff[EffSetTmuxEnv](effs); !ok {
		t.Error("expected EffSetTmuxEnv")
	}
	mustOK(t, effs)
}

func TestPreviewSessionUnknownErrors(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvCmdPreviewSession{ConnID: 1, ReqID: "r", SessionID: "ghost"})
	if _, ok := findEff[EffSendError](effs); !ok {
		t.Error("expected EffSendError")
	}
}

func TestPreviewSessionWithoutWindowErrors(t *testing.T) {
	s := New()
	id := SessionID("abc")
	s.Sessions[id] = Session{ID: id, Driver: stubDriverState{}}
	_, effs := Reduce(s, EvCmdPreviewSession{ConnID: 1, ReqID: "r", SessionID: id})
	if _, ok := findEff[EffSendError](effs); !ok {
		t.Error("expected EffSendError")
	}
}

func TestSwitchSessionAlsoSelectsPane(t *testing.T) {
	s := New()
	id := SessionID("abc")
	s.Sessions[id] = Session{ID: id, WindowID: "@5", Driver: stubDriverState{}}
	_, effs := Reduce(s, EvCmdSwitchSession{ConnID: 1, ReqID: "r", SessionID: id})
	if _, ok := findEff[EffSelectPane](effs); !ok {
		t.Error("expected EffSelectPane")
	}
	mustOK(t, effs)
}

// === reducePreviewProject ===

func TestPreviewProjectDeactivatesActive(t *testing.T) {
	s := New()
	s.Active = "@5"
	next, effs := Reduce(s, EvCmdPreviewProject{ConnID: 1, ReqID: "r", Project: "/foo"})
	if next.Active != "" {
		t.Errorf("active = %q, want empty", next.Active)
	}
	if _, ok := findEff[EffSwapPane](effs); !ok {
		t.Error("expected EffSwapPane to swap back")
	}
	if _, ok := findEff[EffBroadcastEvent](effs); !ok {
		t.Error("expected EffBroadcastEvent")
	}
}

func TestPreviewProjectNoActiveStillBroadcasts(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvCmdPreviewProject{ConnID: 1, ReqID: "r", Project: "/foo"})
	if _, ok := findEff[EffBroadcastEvent](effs); !ok {
		t.Error("expected EffBroadcastEvent")
	}
}

// === reduceListSessions ===

func TestListSessionsResponds(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvCmdListSessions{ConnID: 1, ReqID: "r"})
	if _, ok := findEff[EffSendResponse](effs); !ok {
		t.Error("expected EffSendResponse")
	}
}

// === reduceFocusPane ===

func TestFocusPaneSelectsAndBroadcasts(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvCmdFocusPane{ConnID: 1, ReqID: "r", Pane: "0.1"})
	if _, ok := findEff[EffSelectPane](effs); !ok {
		t.Error("expected EffSelectPane")
	}
	if _, ok := findEff[EffBroadcastEvent](effs); !ok {
		t.Error("expected EffBroadcastEvent")
	}
}

func TestFocusPaneEmptyErrors(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvCmdFocusPane{ConnID: 1, ReqID: "r"})
	if _, ok := findEff[EffSendError](effs); !ok {
		t.Error("expected EffSendError")
	}
}

// === reduceLaunchTool ===

func TestLaunchToolDisplaysPopup(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvCmdLaunchTool{ConnID: 1, ReqID: "r", Tool: "new-session"})
	if _, ok := findEff[EffDisplayPopup](effs); !ok {
		t.Error("expected EffDisplayPopup")
	}
}

func TestLaunchToolEmptyErrors(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvCmdLaunchTool{ConnID: 1, ReqID: "r"})
	if _, ok := findEff[EffSendError](effs); !ok {
		t.Error("expected EffSendError")
	}
}

// === reduceShutdown / reduceDetach ===

func TestShutdownSetsFlag(t *testing.T) {
	s := New()
	next, effs := Reduce(s, EvCmdShutdown{ConnID: 1, ReqID: "r"})
	if !next.ShutdownReq {
		t.Error("ShutdownReq should be true")
	}
	if _, ok := findEff[EffDetachClient](effs); !ok {
		t.Error("expected EffDetachClient")
	}
}

func TestDetach(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvCmdDetach{ConnID: 1, ReqID: "r"})
	if _, ok := findEff[EffDetachClient](effs); !ok {
		t.Error("expected EffDetachClient")
	}
}

// === reduceTmuxWindowVanished ===

func TestTmuxWindowVanishedEvicts(t *testing.T) {
	s := New()
	id := SessionID("abc")
	s.Sessions[id] = Session{ID: id, WindowID: "@5", Driver: stubDriverState{}}
	s.Active = "@5"
	next, effs := Reduce(s, EvTmuxWindowVanished{WindowID: "@5"})
	if _, ok := next.Sessions[id]; ok {
		t.Error("session should be evicted")
	}
	if next.Active != "" {
		t.Errorf("active not cleared: %q", next.Active)
	}
	if _, ok := findEff[EffBroadcastSessionsChanged](effs); !ok {
		t.Error("expected broadcast")
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

// === reduceTranscriptChanged ===

func TestTranscriptChangedRoutes(t *testing.T) {
	s := New()
	id := SessionID("abc")
	s.Sessions[id] = Session{ID: id, Command: "stub", Driver: stubDriverState{}}
	_, effs := Reduce(s, EvTranscriptChanged{SessionID: id, Path: "/x"})
	// stub driver returns no effects, so we expect 0 effects (no
	// persist/broadcast either since the driver did nothing).
	if len(effs) != 0 {
		t.Errorf("expected 0 effects from no-op driver, got %d", len(effs))
	}
}

func TestTranscriptChangedUnknownSession(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvTranscriptChanged{SessionID: "ghost", Path: "/x"})
	if len(effs) != 0 {
		t.Errorf("expected 0 effects, got %d", len(effs))
	}
}

// === reduceHook ===

func TestReduceHookMissingSessionID(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvCmdHook{ConnID: 1, ReqID: "r"})
	if _, ok := findEff[EffSendError](effs); !ok {
		t.Error("expected EffSendError")
	}
}

func TestReduceHookUnknownSession(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvCmdHook{ConnID: 1, ReqID: "r", SessionID: "ghost"})
	if _, ok := findEff[EffSendError](effs); !ok {
		t.Error("expected EffSendError")
	}
}

func TestReduceHookRoutes(t *testing.T) {
	s := New()
	id := SessionID("abc")
	s.Sessions[id] = Session{ID: id, Command: "stub", Driver: stubDriverState{}}
	_, effs := Reduce(s, EvCmdHook{
		ConnID: 1, ReqID: "r", SessionID: id, Event: "session-start",
		Payload: map[string]any{},
	})
	if _, ok := findEff[EffSendResponse](effs); !ok {
		t.Error("expected EffSendResponse")
	}
}

// === postProcessEffect ===

func TestPostProcessAssignsJobID(t *testing.T) {
	s := New()
	s.Now = time.Now()
	patched, next := postProcessEffect(s, "abc", EffStartJob{Input: "test"})
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
	patched, _ := postProcessEffect(s, "abc", EffWatchTranscript{SessionID: "preset", Path: "/x"})
	if patched.(EffWatchTranscript).SessionID != "preset" {
		t.Error("preset SessionID overwritten")
	}
}

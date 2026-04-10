package state

import "fmt"

// Reduce is the pure state transition function. Given a current State
// and an Event, it returns the next State and the side effects the
// runtime should execute.
//
// Reduce never panics in production: every Event variant must have a
// case below. The default branch panics so an unimplemented case
// fails fast in tests rather than silently dropping events.
//
// Reducer cases live in reduce_*.go files split by domain.
func Reduce(s State, ev Event) (State, []Effect) {
	switch e := ev.(type) {
	// session lifecycle
	case EvCmdCreateSession:
		return reduceCreateSession(s, e)
	case EvCmdStopSession:
		return reduceStopSession(s, e)
	case EvCmdPreviewSession:
		return reducePreviewSession(s, e)
	case EvCmdSwitchSession:
		return reduceSwitchSession(s, e)
	case EvCmdPreviewProject:
		return reducePreviewProject(s, e)
	case EvCmdListSessions:
		return reduceListSessions(s, e)
	case EvCmdFocusPane:
		return reduceFocusPane(s, e)
	case EvCmdLaunchTool:
		return reduceLaunchTool(s, e)

	// hook → driver
	case EvCmdHook:
		return reduceHook(s, e)

	// tmux feedback
	case EvTmuxWindowSpawned:
		return reduceTmuxWindowSpawned(s, e)
	case EvTmuxSpawnFailed:
		return reduceTmuxSpawnFailed(s, e)
	case EvTmuxWindowVanished:
		return reduceTmuxWindowVanished(s, e)
	case EvPaneDied:
		return reducePaneDied(s, e)

	// tick
	case EvTick:
		return reduceTick(s, e)

	// worker
	case EvJobResult:
		return reduceJobResult(s, e)

	// fsnotify
	case EvTranscriptChanged:
		return reduceTranscriptChanged(s, e)

	// connection lifecycle
	case EvConnOpened:
		return reduceConnOpened(s, e)
	case EvConnClosed:
		return reduceConnClosed(s, e)
	case EvCmdSubscribe:
		return reduceSubscribe(s, e)
	case EvCmdUnsubscribe:
		return reduceUnsubscribe(s, e)

	// daemon lifecycle
	case EvCmdShutdown:
		return reduceShutdown(s, e)
	case EvCmdDetach:
		return reduceDetach(s, e)
	}

	panic(fmt.Sprintf("state.Reduce: unhandled event type %T", ev))
}

// Reducer cases live in reduce_session.go / reduce_hook.go /
// reduce_tick.go / reduce_job.go / reduce_conn.go / reduce_lifecycle.go.

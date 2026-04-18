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
	// registered command event → dispatch by event type
	case EvEvent:
		return reduceEvent(s, e)
	// driver hook event → route to session's driver
	case EvDriverEvent:
		return reduceDriverHook(s, e)

	// tmux feedback
	case EvTmuxPaneSpawned:
		return reduceTmuxPaneSpawned(s, e)
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
	case EvFileChanged:
		return reduceFileChanged(s, e)

	// pane tap
	case EvPaneActivity:
		return reducePaneActivity(s, e)
	case EvPaneOsc:
		return reducePaneOsc(s, e)

	// connection lifecycle
	case EvConnOpened:
		return reduceConnOpened(s, e)
	case EvConnClosed:
		return reduceConnClosed(s, e)
	case EvCmdSubscribe:
		return reduceSubscribe(s, e)
	case EvCmdUnsubscribe:
		return reduceUnsubscribe(s, e)

	// surface.* / driver.* RPC commands
	case EvCmdSurfaceReadText:
		return reduceSurfaceReadText(s, e)
	case EvCmdSurfaceSendText:
		return reduceSurfaceSendText(s, e)
	case EvCmdSurfaceSendKey:
		return reduceSurfaceSendKey(s, e)
	case EvCmdDriverList:
		return reduceDriverList(s, e)
	}

	panic(fmt.Sprintf("state.Reduce: unhandled event type %T", ev))
}

// Reducer cases live in reduce_event.go / reduce_session.go /
// reduce_tick.go / reduce_job.go / reduce_conn.go / reduce_lifecycle.go.

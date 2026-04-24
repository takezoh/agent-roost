package state

func init() {
	RegisterEvent[struct{}](EventShutdown, reduceShutdown)
	RegisterEvent[struct{}](EventDetach, reduceDetach)
}

func reduceShutdown(s State, connID ConnID, reqID string, _ struct{}) (State, []Effect) {
	return s, []Effect{
		EffPersistSnapshot{},
		EffSendResponseSync{ConnID: connID, ReqID: reqID, Body: nil},
		// Release sandbox resources before killing the tmux session so that
		// docker exec / microVM processes get a clean stop signal rather than
		// being killed via SIGHUP from the pane death.
		EffReleaseFrameSandboxes{},
		EffKillSession{},
	}
}

func reduceDetach(s State, connID ConnID, reqID string, _ struct{}) (State, []Effect) {
	return s, []Effect{
		EffPersistSnapshot{},
		EffSendResponseSync{ConnID: connID, ReqID: reqID, Body: nil},
		EffDetachClient{},
	}
}

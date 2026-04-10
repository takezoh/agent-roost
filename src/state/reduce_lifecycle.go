package state

// Daemon lifecycle reducers. Shutdown sets the ShutdownReq flag so
// the runtime can detect it after Attach returns and kill the tmux
// session. Detach is the soft version that drops the user back to
// their pre-roost terminal without killing anything.

func reduceShutdown(s State, e EvCmdShutdown) (State, []Effect) {
	s.ShutdownReq = true
	return s, []Effect{
		EffPersistSnapshot{},
		okResp(e.ConnID, e.ReqID, nil),
		EffDetachClient{},
	}
}

func reduceDetach(s State, e EvCmdDetach) (State, []Effect) {
	return s, []Effect{
		EffPersistSnapshot{},
		okResp(e.ConnID, e.ReqID, nil),
		EffDetachClient{},
	}
}

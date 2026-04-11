package state

func init() {
	RegisterEvent[struct{}](EventShutdown, reduceShutdown)
	RegisterEvent[struct{}](EventDetach, reduceDetach)
}

func reduceShutdown(s State, connID ConnID, reqID string, _ struct{}) (State, []Effect) {
	s.ShutdownReq = true
	return s, []Effect{
		EffPersistSnapshot{},
		okResp(connID, reqID, nil),
		EffDetachClient{},
	}
}

func reduceDetach(s State, connID ConnID, reqID string, _ struct{}) (State, []Effect) {
	return s, []Effect{
		EffPersistSnapshot{},
		okResp(connID, reqID, nil),
		EffDetachClient{},
	}
}

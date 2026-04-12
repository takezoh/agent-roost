package state

func init() {
	RegisterEvent[struct{}](EventShutdown, reduceShutdown)
	RegisterEvent[struct{}](EventDetach, reduceDetach)
}

func reduceShutdown(s State, connID ConnID, reqID string, _ struct{}) (State, []Effect) {
	return s, []Effect{
		EffPersistSnapshot{},
		EffSendResponseSync{ConnID: connID, ReqID: reqID, Body: nil},
		EffKillSession{},
	}
}

func reduceDetach(s State, connID ConnID, reqID string, _ struct{}) (State, []Effect) {
	return s, []Effect{
		EffPersistSnapshot{},
		okResp(connID, reqID, nil),
		EffDetachClient{},
	}
}

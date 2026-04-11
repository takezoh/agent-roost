package state

import "encoding/json"

var eventHandlers = map[string]func(State, EvEvent) (State, []Effect){}

func RegisterEvent[T any](eventType string, handler func(State, ConnID, string, T) (State, []Effect)) {
	eventHandlers[eventType] = func(s State, e EvEvent) (State, []Effect) {
		var payload T
		if len(e.Payload) > 0 {
			json.Unmarshal(e.Payload, &payload)
		}
		return handler(s, e.ConnID, e.ReqID, payload)
	}
}

func IsRegisteredEvent(name string) bool {
	_, ok := eventHandlers[name]
	return ok
}

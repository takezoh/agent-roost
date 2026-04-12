package state

// Copy-on-write helpers for State maps. Reduce calls these to produce
// new map values rather than mutating in place; the runtime swaps the
// state pointer in one assignment after Reduce returns. With ~10
// sessions the cost is negligible (a few hundred bytes copied per
// event), and the immutability guarantee makes equality testing in
// reducers trivial.

func cloneMap[K comparable, V any](in map[K]V) map[K]V {
	out := make(map[K]V, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneSessions(in map[SessionID]Session) map[SessionID]Session {
	return cloneMap(in)
}

func clonePendingCreates(in map[JobID]PendingCreate) map[JobID]PendingCreate {
	return cloneMap(in)
}

func cloneSubscribers(in map[ConnID]Subscriber) map[ConnID]Subscriber {
	return cloneMap(in)
}

func cloneJobs(in map[JobID]JobMeta) map[JobID]JobMeta {
	return cloneMap(in)
}

func cloneConnectors(in map[string]ConnectorState) map[string]ConnectorState {
	return cloneMap(in)
}

package state

// Copy-on-write helpers for State maps. Reduce calls these to produce
// new map values rather than mutating in place; the runtime swaps the
// state pointer in one assignment after Reduce returns. With ~10
// sessions the cost is negligible (a few hundred bytes copied per
// event), and the immutability guarantee makes equality testing in
// reducers trivial.

func cloneSessions(in map[SessionID]Session) map[SessionID]Session {
	out := make(map[SessionID]Session, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneSubscribers(in map[ConnID]Subscriber) map[ConnID]Subscriber {
	out := make(map[ConnID]Subscriber, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneJobs(in map[JobID]JobMeta) map[JobID]JobMeta {
	out := make(map[JobID]JobMeta, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}

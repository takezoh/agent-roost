package worker

import (
	"log/slog"

	"github.com/takezoh/agent-roost/state"
)

var registry = map[string]func(*Pool, state.JobID, state.JobInput){}

// RegisterRunner registers a typed runner closure for the given job
// kind. Drivers call this at init time — the same pattern as
// state.RegisterTabRenderer.
func RegisterRunner[In state.JobInput, Out any](kind string, runner func(In) (Out, error)) {
	registry[kind] = func(pool *Pool, jobID state.JobID, raw state.JobInput) {
		Submit(pool, jobID, raw.(In), runner)
	}
}

// Dispatch routes a job input to its registered runner and submits it
// to the pool. Called by the runtime effect interpreter.
func Dispatch(pool *Pool, jobID state.JobID, input state.JobInput) {
	fn, ok := registry[input.JobKind()]
	if !ok {
		slog.Warn("worker: no runner registered", "kind", input.JobKind())
		return
	}
	fn(pool, jobID, input)
}

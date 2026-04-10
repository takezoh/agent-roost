// Package worker is the runtime's bounded worker pool. It runs slow
// I/O jobs (haiku summarization, transcript parsing, git branch
// detection, tmux capture-pane) off the runtime event loop and feeds
// results back as state.EvJobResult values.
//
// The pool is created at runtime startup with a fixed worker count
// (default 4). Each worker reads from the same job channel; jobs are
// distributed naturally by Go's runtime. The transcript parser is the
// only stateful job kind — its tracker lives in package globals so
// every worker shares one (the access pattern is naturally serialized
// by the runtime: at most one parse per session in flight at a time).
package worker

import (
	"context"
	"log/slog"
	"sync"

	"github.com/take/agent-roost/lib/claude/transcript"
	"github.com/take/agent-roost/state"
)

// Job is the unit of work submitted to the pool. ID and Kind come
// from state.EffStartJob; Input is the typed input value the worker
// type-asserts into its concrete request struct.
type Job struct {
	ID    state.JobID
	Kind  state.JobKind
	Input any
}

// Deps is the dependency bag every worker shares. Tracker is package-
// global so it survives across jobs (incremental parsing).
type Deps struct {
	Tmux         TmuxOps         // capture-pane backend
	Haiku        HaikuRunner     // claude -p subprocess runner
	Branch       BranchDetector  // git branch detector
	TranscriptDB *TranscriptStore // shared incremental parser
}

// TmuxOps is the narrow tmux interface workers need.
type TmuxOps interface {
	CapturePane(windowID string, nLines int) (string, error)
}

// HaikuRunner runs the haiku summarizer subprocess. Defined as an
// interface so tests can swap a stub.
type HaikuRunner interface {
	Summarize(ctx context.Context, prompt string) (string, error)
}

// BranchDetector detects the git branch for a working directory.
type BranchDetector interface {
	Detect(workingDir string) string
}

// TranscriptStore wraps a transcript.Tracker with a mutex so multiple
// workers can safely call Parse for different sessions in parallel.
// Same-session parses are naturally serialized by the runtime.
type TranscriptStore struct {
	mu      sync.Mutex
	tracker *transcript.Tracker
}

// NewTranscriptStore returns an empty store backed by a fresh tracker.
func NewTranscriptStore() *TranscriptStore {
	return &TranscriptStore{tracker: transcript.NewTracker()}
}

// Parse runs an incremental update on the tracker and returns the
// resulting MetaSnapshot + status line.
func (t *TranscriptStore) Parse(claudeUUID, path string) (transcript.MetaSnapshot, string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, err := t.tracker.Update(claudeUUID, path); err != nil {
		return transcript.MetaSnapshot{}, "", err
	}
	return t.tracker.Snapshot(claudeUUID), t.tracker.StatusLine(claudeUUID), nil
}

// Forget releases per-session state.
func (t *TranscriptStore) Forget(claudeUUID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.tracker.Forget(claudeUUID)
}

// Pool is the bounded worker pool. Submit puts a job on the queue;
// Results returns the channel workers post completion events on.
type Pool struct {
	deps    Deps
	jobs    chan Job
	results chan state.Event
	stop    chan struct{}
	stopped chan struct{}
	closed  bool
	mu      sync.Mutex
}

// NewPool starts `size` worker goroutines.
func NewPool(size int, deps Deps) *Pool {
	if deps.TranscriptDB == nil {
		deps.TranscriptDB = NewTranscriptStore()
	}
	p := &Pool{
		deps:    deps,
		jobs:    make(chan Job, 64),
		results: make(chan state.Event, 64),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	var wg sync.WaitGroup
	for i := 0; i < size; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.run()
		}()
	}
	go func() {
		wg.Wait()
		close(p.stopped)
	}()
	return p
}

// Submit enqueues a job. Drops with a warning if the queue is full
// (workers are saturated by a flood of jobs); the runtime will
// receive no EvJobResult and the in-flight flag on the driver will
// stay set until next reset.
func (p *Pool) Submit(j Job) {
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return
	}
	select {
	case p.jobs <- j:
	default:
		slog.Warn("worker: job queue full, dropping",
			"job_id", j.ID, "kind", j.Kind.String())
	}
}

// Results returns the channel that workers post EvJobResult events on.
func (p *Pool) Results() <-chan state.Event { return p.results }

// Stop drains the queue, signals workers to exit, and waits for them
// to finish. Idempotent.
func (p *Pool) Stop() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	close(p.stop)
	p.mu.Unlock()
	<-p.stopped
}

// run is the per-worker loop. Each iteration pulls one job, runs it,
// and posts the result. On stop, drains any already-queued jobs so
// callers waiting on results don't hang forever.
func (p *Pool) run() {
	for {
		select {
		case <-p.stop:
			// Drain remaining jobs so submit() callers see results.
			for {
				select {
				case j := <-p.jobs:
					p.runJob(j)
				default:
					return
				}
			}
		case j := <-p.jobs:
			p.runJob(j)
		}
	}
}

func (p *Pool) runJob(j Job) {
	result, err := p.execute(j)
	ev := state.EvJobResult{
		JobID:  j.ID,
		Result: result,
		Err:    err,
	}
	select {
	case p.results <- ev:
	case <-p.stop:
	}
}

func (p *Pool) execute(j Job) (any, error) {
	switch j.Kind {
	case state.JobCapturePane:
		return runCapturePane(p.deps, j.Input)
	case state.JobHaikuSummary:
		return runHaikuSummary(p.deps, j.Input)
	case state.JobTranscriptParse:
		return runTranscriptParse(p.deps, j.Input)
	case state.JobGitBranch:
		return runGitBranch(p.deps, j.Input)
	}
	return nil, errUnknownKind(j.Kind)
}

type errKind state.JobKind

func (e errKind) Error() string {
	return "worker: unknown job kind: " + state.JobKind(e).String()
}

func errUnknownKind(k state.JobKind) error { return errKind(k) }

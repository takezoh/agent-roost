package worker

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/take/agent-roost/state"
)

type job struct {
	id  state.JobID
	run func() (any, error)
}

type Pool struct {
	jobs    chan job
	results chan state.Event
	stop    chan struct{}
	stopped chan struct{}
	closed  bool
	mu      sync.Mutex
}

func NewPool(size int) *Pool {
	p := &Pool{
		jobs:    make(chan job, 64),
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

// Submit enqueues a typed job. The runner closure is bound at the call
// site with concrete In/Out types, so no reflect dispatch is needed.
func Submit[In, Out any](p *Pool, id state.JobID, input In, runner func(In) (Out, error)) {
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return
	}
	j := job{
		id:  id,
		run: func() (any, error) { return runner(input) },
	}
	select {
	case p.jobs <- j:
	default:
		slog.Warn("worker: job queue full, dropping",
			"job_id", id, "input", fmt.Sprintf("%T", input))
	}
}

func (p *Pool) Results() <-chan state.Event { return p.results }

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

func (p *Pool) run() {
	for {
		select {
		case <-p.stop:
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

func (p *Pool) runJob(j job) {
	result, err := j.run()
	ev := state.EvJobResult{
		JobID:  j.id,
		Result: result,
		Err:    err,
	}
	select {
	case p.results <- ev:
	case <-p.stop:
	}
}

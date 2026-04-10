package worker

import (
	"fmt"
	"log/slog"
	"reflect"
	"sync"

	"github.com/take/agent-roost/state"
)

// Executor is a type-based runner registry. Each runner is registered
// by the reflect.Type of its input value. Run dispatches by looking
// up the input's concrete type — no switch required.
type Executor struct {
	runners map[reflect.Type]func(any) (any, error)
}

// NewExecutor returns an empty Executor. Populate it with Register
// before passing to NewPool.
func NewExecutor() *Executor {
	return &Executor{runners: make(map[reflect.Type]func(any) (any, error))}
}

// Register maps the concrete type of inputSample to runner. Panics
// on duplicate registrations to fail fast at startup.
func (e *Executor) Register(inputSample any, runner func(any) (any, error)) {
	t := reflect.TypeOf(inputSample)
	if _, exists := e.runners[t]; exists {
		panic(fmt.Sprintf("worker: duplicate runner for %v", t))
	}
	e.runners[t] = runner
}

// Run dispatches input to the registered runner by its concrete type.
func (e *Executor) Run(input any) (any, error) {
	t := reflect.TypeOf(input)
	runner, ok := e.runners[t]
	if !ok {
		return nil, fmt.Errorf("worker: no runner for %v", t)
	}
	return runner(input)
}

type Job struct {
	ID    state.JobID
	Input any
}

type Pool struct {
	exec    *Executor
	jobs    chan Job
	results chan state.Event
	stop    chan struct{}
	stopped chan struct{}
	closed  bool
	mu      sync.Mutex
}

func NewPool(size int, exec *Executor) *Pool {
	p := &Pool{
		exec:    exec,
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
			"job_id", j.ID, "input", fmt.Sprintf("%T", j.Input))
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

func (p *Pool) runJob(j Job) {
	result, err := p.exec.Run(j.Input)
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

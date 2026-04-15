package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/takezoh/agent-roost/state"
)

type testInput struct{ Val string }

func (testInput) JobKind() string { return "test" }

type testOutput struct{ Result string }

func TestPoolRoundTrip(t *testing.T) {
	pool := NewPool(context.Background(), 2)
	defer pool.Stop()

	runner := func(ctx context.Context, in testInput) (testOutput, error) {
		return testOutput{Result: "got:" + in.Val}, nil
	}

	Submit(pool, 1, testInput{Val: "hello"}, runner)

	select {
	case ev := <-pool.Results():
		res := ev.(state.EvJobResult)
		if res.JobID != 1 {
			t.Errorf("JobID = %d", res.JobID)
		}
		if res.Err != nil {
			t.Errorf("Err = %v", res.Err)
		}
		out := res.Result.(testOutput)
		if out.Result != "got:hello" {
			t.Errorf("Result = %q", out.Result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestRegistryDispatch(t *testing.T) {
	RegisterRunner("test", func(ctx context.Context, in testInput) (testOutput, error) {
		return testOutput{Result: "dispatched:" + in.Val}, nil
	})

	pool := NewPool(context.Background(), 1)
	defer pool.Stop()

	Dispatch(pool, 42, testInput{Val: "abc"})

	select {
	case ev := <-pool.Results():
		res := ev.(state.EvJobResult)
		if res.JobID != 42 {
			t.Errorf("JobID = %d", res.JobID)
		}
		out := res.Result.(testOutput)
		if out.Result != "dispatched:abc" {
			t.Errorf("Result = %q", out.Result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDispatchUnregisteredKind(t *testing.T) {
	pool := NewPool(context.Background(), 1)
	defer pool.Stop()

	// unknownInput does not have a registered runner — Dispatch should
	// log a warning and not panic.
	Dispatch(pool, 99, stubInput{})

	select {
	case <-pool.Results():
		t.Fatal("unexpected result for unregistered kind")
	case <-time.After(50 * time.Millisecond):
		// expected: no result
	}
}

type stubInput struct{}

func (stubInput) JobKind() string { return "unregistered_kind" }

func TestPoolStopIsIdempotent(t *testing.T) {
	pool := NewPool(context.Background(), 2)
	pool.Stop()
	pool.Stop()
}

func TestPoolSubmitAfterStopDrops(t *testing.T) {
	pool := NewPool(context.Background(), 1)
	pool.Stop()
	Submit(pool, 99, testInput{}, func(ctx context.Context, in testInput) (testOutput, error) {
		return testOutput{}, nil
	})
}

// TestPoolStopCancelsRunningJob verifies that Stop cancels the pool ctx
// and a ctx-aware runner exits promptly.
func TestPoolStopCancelsRunningJob(t *testing.T) {
	pool := NewPool(context.Background(), 1)

	started := make(chan struct{})
	runner := func(ctx context.Context, in testInput) (testOutput, error) {
		close(started)
		<-ctx.Done()
		return testOutput{}, ctx.Err()
	}
	Submit(pool, 1, testInput{}, runner)

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not start")
	}

	done := make(chan struct{})
	go func() {
		pool.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Stop did not return within 200ms after runner observed ctx cancel")
	}
}

// TestPoolStopDropsQueuedJobs verifies that jobs queued but not yet
// started are not executed after Stop.
func TestPoolStopDropsQueuedJobs(t *testing.T) {
	// 1 worker, slow job occupies it, queued job should be dropped.
	pool := NewPool(context.Background(), 1)

	started := make(chan struct{})
	// First job blocks until ctx is cancelled.
	Submit(pool, 1, testInput{}, func(ctx context.Context, in testInput) (testOutput, error) {
		close(started)
		<-ctx.Done()
		return testOutput{}, nil
	})

	// Wait until the blocking job has started so the worker is occupied.
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first job did not start")
	}

	var executed atomic.Int32
	Submit(pool, 2, testInput{}, func(ctx context.Context, in testInput) (testOutput, error) {
		executed.Add(1)
		return testOutput{}, nil
	})

	pool.Stop()

	if executed.Load() != 0 {
		t.Errorf("queued job was executed after Stop, want 0 executions")
	}
}

// TestPoolStopHardDeadline verifies that Stop returns within the 700ms
// budget even when a runner ignores the ctx (500ms deadline + slack).
func TestPoolStopHardDeadline(t *testing.T) {
	pool := NewPool(context.Background(), 1)

	Submit(pool, 1, testInput{}, func(ctx context.Context, in testInput) (testOutput, error) {
		// Deliberately ignores ctx — simulates a misbehaving runner.
		time.Sleep(5 * time.Second)
		return testOutput{}, nil
	})

	// Give the runner a moment to start.
	time.Sleep(20 * time.Millisecond)

	start := time.Now()
	pool.Stop()
	elapsed := time.Since(start)

	if elapsed > 700*time.Millisecond {
		t.Errorf("Stop took %v, want <700ms", elapsed)
	}
}

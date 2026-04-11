package worker

import (
	"testing"
	"time"

	"github.com/takezoh/agent-roost/state"
)

type testInput struct{ Val string }

func (testInput) JobKind() string { return "test" }

type testOutput struct{ Result string }

func TestPoolRoundTrip(t *testing.T) {
	pool := NewPool(2)
	defer pool.Stop()

	runner := func(in testInput) (testOutput, error) {
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
	RegisterRunner("test", func(in testInput) (testOutput, error) {
		return testOutput{Result: "dispatched:" + in.Val}, nil
	})

	pool := NewPool(1)
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
	pool := NewPool(1)
	defer pool.Stop()

	type unknownInput struct{}
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
	pool := NewPool(2)
	pool.Stop()
	pool.Stop()
}

func TestPoolSubmitAfterStopDrops(t *testing.T) {
	pool := NewPool(1)
	pool.Stop()
	Submit(pool, 99, testInput{}, func(in testInput) (testOutput, error) {
		return testOutput{}, nil
	})
}

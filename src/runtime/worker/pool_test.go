package worker

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/take/agent-roost/state"
	"github.com/take/agent-roost/state/driver"
)

func testExec() *Executor {
	exec := NewExecutor()
	exec.Register(driver.CapturePaneInput{}, func(input any) (any, error) {
		content := "$ "
		h := sha256.Sum256([]byte(content))
		return driver.CapturePaneResult{Content: content, Hash: hex.EncodeToString(h[:])}, nil
	})
	exec.Register(driver.HaikuSummaryInput{}, func(input any) (any, error) {
		return driver.HaikuSummaryResult{Summary: "short summary"}, nil
	})
	exec.Register(driver.GitBranchInput{}, func(input any) (any, error) {
		return driver.GitBranchResult{Branch: "feature/x"}, nil
	})
	return exec
}

func TestPoolCapturePaneRoundTrip(t *testing.T) {
	pool := NewPool(2, testExec())
	defer pool.Stop()

	pool.Submit(Job{
		ID:    1,
		Input: driver.CapturePaneInput{WindowID: "@5", NLines: 5},
	})

	select {
	case ev := <-pool.Results():
		res := ev.(state.EvJobResult)
		if res.JobID != 1 {
			t.Errorf("JobID = %d", res.JobID)
		}
		if res.Err != nil {
			t.Errorf("Err = %v", res.Err)
		}
		cpr := res.Result.(driver.CapturePaneResult)
		if cpr.Content != "$ " {
			t.Errorf("Content = %q", cpr.Content)
		}
		if cpr.Hash == "" {
			t.Error("Hash should be set")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestPoolHaikuRoundTrip(t *testing.T) {
	pool := NewPool(2, testExec())
	defer pool.Stop()

	pool.Submit(Job{
		ID:    2,
		Input: driver.HaikuSummaryInput{CurrentPrompt: "test"},
	})

	select {
	case ev := <-pool.Results():
		res := ev.(state.EvJobResult)
		hr := res.Result.(driver.HaikuSummaryResult)
		if hr.Summary != "short summary" {
			t.Errorf("Summary = %q", hr.Summary)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestPoolGitBranch(t *testing.T) {
	pool := NewPool(1, testExec())
	defer pool.Stop()

	pool.Submit(Job{
		ID:    4,
		Input: driver.GitBranchInput{WorkingDir: "/tmp"},
	})

	select {
	case ev := <-pool.Results():
		res := ev.(state.EvJobResult)
		if res.Err != nil {
			t.Fatalf("err = %v", res.Err)
		}
		gbr := res.Result.(driver.GitBranchResult)
		if gbr.Branch != "feature/x" {
			t.Errorf("Branch = %q", gbr.Branch)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestPoolUnknownInputReturnsError(t *testing.T) {
	pool := NewPool(1, testExec())
	defer pool.Stop()

	pool.Submit(Job{ID: 5, Input: 42})

	select {
	case ev := <-pool.Results():
		res := ev.(state.EvJobResult)
		if res.Err == nil {
			t.Error("expected error for unknown input type")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestPoolStopIsIdempotent(t *testing.T) {
	pool := NewPool(2, testExec())
	pool.Stop()
	pool.Stop()
}

func TestPoolSubmitAfterStopDrops(t *testing.T) {
	pool := NewPool(1, testExec())
	pool.Stop()
	pool.Submit(Job{ID: 99, Input: "test"})
}

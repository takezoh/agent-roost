package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/take/agent-roost/state"
	"github.com/take/agent-roost/state/driver"
)

type fakeTmux struct {
	calls   int
	content string
	err     error
}

func (f *fakeTmux) CapturePane(string, int) (string, error) {
	f.calls++
	return f.content, f.err
}

type fakeHaiku struct {
	resp string
	err  error
}

func (f fakeHaiku) Summarize(ctx context.Context, prompt string) (string, error) {
	return f.resp, f.err
}

type fakeBranch struct{ branch string }

func (f fakeBranch) Detect(string) string { return f.branch }

func TestPoolCapturePaneRoundTrip(t *testing.T) {
	tmux := &fakeTmux{content: "$ "}
	pool := NewPool(2, Deps{Tmux: tmux})
	defer pool.Stop()

	pool.Submit(Job{
		ID:    1,
		Kind:  state.JobCapturePane,
		Input: driver.CapturePaneInput{WindowID: "@5", NLines: 5},
	})

	select {
	case ev := <-pool.Results():
		res, ok := ev.(state.EvJobResult)
		if !ok {
			t.Fatalf("ev type = %T", ev)
		}
		if res.JobID != 1 {
			t.Errorf("JobID = %d", res.JobID)
		}
		if res.Err != nil {
			t.Errorf("Err = %v", res.Err)
		}
		cpr, ok := res.Result.(driver.CapturePaneResult)
		if !ok {
			t.Fatalf("result type = %T", res.Result)
		}
		if cpr.Content != "$ " {
			t.Errorf("Content = %q", cpr.Content)
		}
		if cpr.Hash == "" {
			t.Error("Hash should be set")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	if tmux.calls != 1 {
		t.Errorf("CapturePane called %d times", tmux.calls)
	}
}

func TestPoolHaikuRoundTrip(t *testing.T) {
	pool := NewPool(2, Deps{Haiku: fakeHaiku{resp: "短い要約\n"}})
	defer pool.Stop()

	pool.Submit(Job{
		ID:    2,
		Kind:  state.JobHaikuSummary,
		Input: driver.HaikuSummaryInput{Prompt: "test"},
	})

	select {
	case ev := <-pool.Results():
		res := ev.(state.EvJobResult)
		hr := res.Result.(driver.HaikuSummaryResult)
		if hr.Summary != "短い要約" {
			t.Errorf("Summary = %q (should be trimmed)", hr.Summary)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestPoolHaikuError(t *testing.T) {
	pool := NewPool(1, Deps{Haiku: fakeHaiku{err: errors.New("boom")}})
	defer pool.Stop()

	pool.Submit(Job{
		ID:    3,
		Kind:  state.JobHaikuSummary,
		Input: driver.HaikuSummaryInput{Prompt: "test"},
	})

	select {
	case ev := <-pool.Results():
		res := ev.(state.EvJobResult)
		if res.Err == nil {
			t.Error("expected error result")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestPoolGitBranch(t *testing.T) {
	pool := NewPool(1, Deps{Branch: fakeBranch{branch: "feature/x"}})
	defer pool.Stop()

	pool.Submit(Job{
		ID:    4,
		Kind:  state.JobGitBranch,
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

func TestPoolUnknownKindReturnsError(t *testing.T) {
	pool := NewPool(1, Deps{})
	defer pool.Stop()

	pool.Submit(Job{ID: 5, Kind: state.JobUnknown, Input: nil})

	select {
	case ev := <-pool.Results():
		res := ev.(state.EvJobResult)
		if res.Err == nil {
			t.Error("expected error for unknown job kind")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestPoolStopIsIdempotent(t *testing.T) {
	pool := NewPool(2, Deps{})
	pool.Stop()
	pool.Stop() // should not panic
}

func TestPoolSubmitAfterStopDrops(t *testing.T) {
	pool := NewPool(1, Deps{})
	pool.Stop()
	// Should not panic, just silently drop.
	pool.Submit(Job{ID: 99, Kind: state.JobHaikuSummary})
}

func TestCapturePaneWrongInputType(t *testing.T) {
	pool := NewPool(1, Deps{Tmux: &fakeTmux{}})
	defer pool.Stop()

	pool.Submit(Job{
		ID:    6,
		Kind:  state.JobCapturePane,
		Input: "not a CapturePaneInput",
	})
	select {
	case ev := <-pool.Results():
		res := ev.(state.EvJobResult)
		if res.Err == nil {
			t.Error("expected type assertion error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

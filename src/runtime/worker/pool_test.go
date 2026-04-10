package worker

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/take/agent-roost/driver"
	"github.com/take/agent-roost/state"
)

func testRunners() Runners {
	return Runners{
		CapturePane: func(in driver.CapturePaneInput) (driver.CapturePaneResult, error) {
			content := "$ "
			h := sha256.Sum256([]byte(content))
			return driver.CapturePaneResult{Content: content, Hash: hex.EncodeToString(h[:])}, nil
		},
		HaikuSummary: func(in driver.HaikuSummaryInput) (driver.HaikuSummaryResult, error) {
			return driver.HaikuSummaryResult{Summary: "short summary"}, nil
		},
		GitBranch: func(in driver.GitBranchInput) (driver.GitBranchResult, error) {
			return driver.GitBranchResult{Branch: "feature/x"}, nil
		},
		TranscriptParse: func(in driver.TranscriptParseInput) (driver.TranscriptParseResult, error) {
			return driver.TranscriptParseResult{Title: "test"}, nil
		},
	}
}

func TestPoolCapturePaneRoundTrip(t *testing.T) {
	pool := NewPool(2)
	defer pool.Stop()
	runners := testRunners()

	Submit(pool, 1, driver.CapturePaneInput{WindowID: "@5", NLines: 5}, runners.CapturePane)

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
	pool := NewPool(2)
	defer pool.Stop()
	runners := testRunners()

	Submit(pool, 2, driver.HaikuSummaryInput{CurrentPrompt: "test"}, runners.HaikuSummary)

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
	pool := NewPool(1)
	defer pool.Stop()
	runners := testRunners()

	Submit(pool, 4, driver.GitBranchInput{WorkingDir: "/tmp"}, runners.GitBranch)

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

func TestPoolStopIsIdempotent(t *testing.T) {
	pool := NewPool(2)
	pool.Stop()
	pool.Stop()
}

func TestPoolSubmitAfterStopDrops(t *testing.T) {
	pool := NewPool(1)
	pool.Stop()
	runners := testRunners()
	Submit(pool, 99, driver.CapturePaneInput{}, runners.CapturePane)
}

package connector

import (
	"errors"
	"testing"
	"time"

	"github.com/takezoh/agent-roost/state"
)

func TestGitHubStepTickEmitsJobWhenStale(t *testing.T) {
	c := GitHubConnector{}
	gs := c.NewState()

	next, effs := c.Step(gs, state.CEvTick{Now: time.Now()})
	if len(effs) != 1 {
		t.Fatalf("expected 1 effect, got %d", len(effs))
	}
	if _, ok := effs[0].(state.EffStartJob); !ok {
		t.Fatalf("expected EffStartJob, got %T", effs[0])
	}
	// Fetching should be true now.
	gs2 := next.(GitHubState)
	if !gs2.Fetching {
		t.Error("Fetching should be true after emitting job")
	}
}

func TestGitHubStepTickSkipsWhenFetching(t *testing.T) {
	c := GitHubConnector{}
	gs := GitHubState{Fetching: true}

	_, effs := c.Step(gs, state.CEvTick{Now: time.Now()})
	if len(effs) != 0 {
		t.Fatalf("expected 0 effects when fetching, got %d", len(effs))
	}
}

func TestGitHubStepTickSkipsWhenFresh(t *testing.T) {
	c := GitHubConnector{}
	now := time.Now()
	gs := GitHubState{FetchedAt: now.Add(-30 * time.Second)}

	_, effs := c.Step(gs, state.CEvTick{Now: now})
	if len(effs) != 0 {
		t.Fatalf("expected 0 effects when cache fresh, got %d", len(effs))
	}
}

func TestGitHubStepJobResultSuccess(t *testing.T) {
	c := GitHubConnector{}
	gs := GitHubState{Fetching: true}
	now := time.Now()

	result := GitHubFetchResult{
		PRs:    []PRInfo{{Number: 1, Title: "Fix bug", Repo: "org/repo"}},
		Issues: []IssueInfo{{Number: 2, Title: "Feature", Repo: "org/repo"}},
	}
	next, _ := c.Step(gs, state.CEvJobResult{Result: result, Now: now})
	gs2 := next.(GitHubState)

	if gs2.Fetching {
		t.Error("Fetching should be false after result")
	}
	if !gs2.Available {
		t.Error("Available should be true after success")
	}
	if len(gs2.PRs) != 1 {
		t.Errorf("PRs length = %d, want 1", len(gs2.PRs))
	}
	if len(gs2.Issues) != 1 {
		t.Errorf("Issues length = %d, want 1", len(gs2.Issues))
	}
}

func TestGitHubStepJobResultError(t *testing.T) {
	c := GitHubConnector{}
	gs := GitHubState{Fetching: true}

	next, _ := c.Step(gs, state.CEvJobResult{Err: errors.New("fail"), Now: time.Now()})
	gs2 := next.(GitHubState)

	if gs2.Fetching {
		t.Error("Fetching should be false after error")
	}
	if gs2.Available {
		t.Error("Available should be false after error")
	}
}

func TestGitHubViewAvailable(t *testing.T) {
	c := GitHubConnector{}
	gs := GitHubState{
		Available: true,
		PRs:       []PRInfo{{Number: 1, Title: "Fix", Repo: "r"}},
		Issues:    []IssueInfo{{Number: 2, Title: "Bug", Repo: "r"}, {Number: 3, Title: "Feat", Repo: "r"}},
	}
	view := c.View(gs)
	if !view.Available {
		t.Error("view should be available")
	}
	if view.Summary != "1 PR · 2 Issues" {
		t.Errorf("Summary = %q, want %q", view.Summary, "1 PR · 2 Issues")
	}
	if len(view.Sections) != 2 {
		t.Fatalf("Sections length = %d, want 2", len(view.Sections))
	}
}

func TestGitHubViewUnavailable(t *testing.T) {
	c := GitHubConnector{}
	gs := GitHubState{Available: false}
	view := c.View(gs)
	if view.Available {
		t.Error("view should not be available")
	}
}

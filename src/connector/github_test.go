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
		Runs:   []RunInfo{{Name: "CI", Status: "completed", Conclusion: "failure", Repo: "org/repo", Branch: "main"}},
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
	if len(gs2.Runs) != 1 {
		t.Errorf("Runs length = %d, want 1", len(gs2.Runs))
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

func TestGitHubViewWithRuns(t *testing.T) {
	c := GitHubConnector{}
	gs := GitHubState{
		Available: true,
		PRs:       []PRInfo{{Number: 1, Title: "Fix", Repo: "r"}},
		Issues:    []IssueInfo{},
		Runs:      []RunInfo{{Name: "CI", Status: "completed", Conclusion: "failure", Repo: "org/repo", Branch: "main"}},
	}
	view := c.View(gs)
	if view.Summary != "1 PR · 0 Issues · 1 Run" {
		t.Errorf("Summary = %q, want %q", view.Summary, "1 PR · 0 Issues · 1 Run")
	}
	if len(view.Sections) != 2 {
		t.Fatalf("Sections length = %d, want 2 (Actions + PRs)", len(view.Sections))
	}
	if view.Sections[0].Title != "Actions (1)" {
		t.Errorf("first section title = %q, want %q", view.Sections[0].Title, "Actions (1)")
	}
}

func TestGitHubViewNoRuns(t *testing.T) {
	c := GitHubConnector{}
	gs := GitHubState{
		Available: true,
		PRs:       []PRInfo{{Number: 1, Title: "Fix", Repo: "r"}},
		Issues:    []IssueInfo{{Number: 2, Title: "Bug", Repo: "r"}},
		Runs:      []RunInfo{},
	}
	view := c.View(gs)
	if view.Summary != "1 PR · 1 Issue" {
		t.Errorf("Summary = %q, want %q", view.Summary, "1 PR · 1 Issue")
	}
	for _, sec := range view.Sections {
		if sec.Title == "Actions (0)" {
			t.Error("should not have empty Actions section")
		}
	}
}

func TestToRunItemsSymbols(t *testing.T) {
	runs := []RunInfo{
		{Name: "CI", Status: "completed", Conclusion: "failure", Repo: "r", Branch: "main"},
		{Name: "Deploy", Status: "in_progress", Repo: "r", Branch: "dev"},
		{Name: "Lint", Status: "queued", Repo: "r", Branch: "feat"},
		{Name: "Test", Status: "waiting", Repo: "r", Branch: "feat"},
	}
	items := toRunItems(runs)
	wantSymbols := []string{"✗", "▶", "◌", "◌"}
	for i, item := range items {
		if item.Symbol != wantSymbols[i] {
			t.Errorf("items[%d].Symbol = %q, want %q", i, item.Symbol, wantSymbols[i])
		}
	}
}

func TestToRunItemsTruncatesLongName(t *testing.T) {
	long := "This is a very long workflow name that exceeds fifty characters easily"
	runs := []RunInfo{{Name: long, Status: "in_progress", Repo: "r", Branch: "main"}}
	items := toRunItems(runs)
	r := []rune(items[0].Title)
	if len(r) > 50 {
		t.Errorf("title length = %d runes, want <= 50", len(r))
	}
	if r[len(r)-1] != '…' {
		t.Error("truncated title should end with …")
	}
}

func TestToRunItemsMeta(t *testing.T) {
	runs := []RunInfo{{Name: "CI", Status: "in_progress", Repo: "org/repo", Branch: "main", Age: 3 * time.Hour}}
	items := toRunItems(runs)
	want := "org/repo · main · 3h"
	if items[0].Meta != want {
		t.Errorf("Meta = %q, want %q", items[0].Meta, want)
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

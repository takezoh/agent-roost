package connector

import (
	"fmt"
	"time"

	"github.com/takezoh/agent-roost/state"
)

const githubFetchInterval = 2 * time.Minute

type GitHubConnector struct{}

var _ state.Connector = GitHubConnector{}

func (GitHubConnector) Name() string        { return "github" }
func (GitHubConnector) DisplayName() string { return "GitHub" }

func (GitHubConnector) NewState() state.ConnectorState {
	return GitHubState{}
}

func (GitHubConnector) Step(prev state.ConnectorState, ev state.ConnectorEvent) (state.ConnectorState, []state.Effect) {
	gs, ok := prev.(GitHubState)
	if !ok {
		gs = GitHubState{}
	}

	switch e := ev.(type) {
	case state.CEvTick:
		return githubStepTick(gs, e)
	case state.CEvJobResult:
		return githubStepJobResult(gs, e)
	}
	return gs, nil
}

func (GitHubConnector) View(s state.ConnectorState) state.ConnectorView {
	gs, ok := s.(GitHubState)
	if !ok {
		return state.ConnectorView{Label: "GitHub"}
	}
	if !gs.Available {
		return state.ConnectorView{Label: "GitHub"}
	}

	summary := fmt.Sprintf("%d %s · %d %s",
		len(gs.PRs), plural(len(gs.PRs), "PR", "PRs"),
		len(gs.Issues), plural(len(gs.Issues), "Issue", "Issues"))
	prItems, issueItems := toItems(gs.PRs, gs.Issues)

	var sections []state.ConnectorSection
	if len(prItems) > 0 {
		sections = append(sections, state.ConnectorSection{
			Title: fmt.Sprintf("Pull Requests (%d)", len(gs.PRs)),
			Items: prItems,
		})
	}
	if len(issueItems) > 0 {
		sections = append(sections, state.ConnectorSection{
			Title: fmt.Sprintf("Issues (%d)", len(gs.Issues)),
			Items: issueItems,
		})
	}

	return state.ConnectorView{
		Label:     "GitHub",
		Summary:   summary,
		Available: true,
		Sections:  sections,
	}
}

func plural(n int, singular, p string) string {
	if n == 1 {
		return singular
	}
	return p
}

func githubStepTick(gs GitHubState, e state.CEvTick) (state.ConnectorState, []state.Effect) {
	if gs.Fetching {
		return gs, nil
	}
	if !gs.FetchedAt.IsZero() && e.Now.Sub(gs.FetchedAt) < githubFetchInterval {
		return gs, nil
	}
	gs.Fetching = true
	return gs, []state.Effect{
		state.EffStartJob{Input: GitHubFetchInput{}},
	}
}

func githubStepJobResult(gs GitHubState, e state.CEvJobResult) (state.ConnectorState, []state.Effect) {
	gs.Fetching = false
	gs.FetchedAt = e.Now

	if e.Err != nil {
		gs.Available = false
		return gs, nil
	}

	result, ok := e.Result.(GitHubFetchResult)
	if !ok {
		gs.Available = false
		return gs, nil
	}

	gs.Available = true
	gs.PRs = result.PRs
	gs.Issues = result.Issues
	return gs, nil
}

package connector

import (
	"fmt"

	"github.com/takezoh/agent-roost/state"
)

func (GitHubConnector) View(s state.ConnectorState) state.ConnectorView {
	gs, ok := s.(GitHubState)
	if !ok || !gs.Available {
		return state.ConnectorView{Label: "GitHub"}
	}

	parts := []string{
		fmt.Sprintf("%d %s", len(gs.PRs), plural(len(gs.PRs), "PR", "PRs")),
		fmt.Sprintf("%d %s", len(gs.Issues), plural(len(gs.Issues), "Issue", "Issues")),
	}
	if len(gs.Runs) > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", len(gs.Runs), plural(len(gs.Runs), "Run", "Runs")))
	}
	summary := joinParts(parts)

	return state.ConnectorView{
		Label:     "GitHub",
		Summary:   summary,
		Available: true,
		Sections:  buildSections(gs),
	}
}

func buildSections(gs GitHubState) []state.ConnectorSection {
	prItems, issueItems := toItems(gs.PRs, gs.Issues)
	runItems := toRunItems(gs.Runs)

	var sections []state.ConnectorSection
	if len(runItems) > 0 {
		sections = append(sections, state.ConnectorSection{
			Title: fmt.Sprintf("Actions (%d)", len(gs.Runs)),
			Items: runItems,
		})
	}
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
	return sections
}

func plural(n int, singular, p string) string {
	if n == 1 {
		return singular
	}
	return p
}

func joinParts(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += " · " + p
	}
	return result
}

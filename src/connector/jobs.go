package connector

import (
	"fmt"
	"time"

	"github.com/takezoh/agent-roost/state"
)

type GitHubFetchInput struct{}

type GitHubFetchResult struct {
	PRs    []PRInfo
	Issues []IssueInfo
}

func (GitHubFetchInput) JobKind() string { return "github_fetch" }

var _ state.JobInput = GitHubFetchInput{}

func toItems(prs []PRInfo, issues []IssueInfo) ([]state.ConnectorItem, []state.ConnectorItem) {
	prItems := make([]state.ConnectorItem, len(prs))
	for i, pr := range prs {
		prItems[i] = state.ConnectorItem{
			Symbol: "●",
			Title:  formatItemTitle(pr.Number, pr.Title),
			Meta:   pr.Repo + " · " + formatAge(pr.Age),
		}
	}
	issueItems := make([]state.ConnectorItem, len(issues))
	for i, iss := range issues {
		issueItems[i] = state.ConnectorItem{
			Symbol: "●",
			Title:  formatItemTitle(iss.Number, iss.Title),
			Meta:   iss.Repo + " · " + formatAge(iss.Age),
		}
	}
	return prItems, issueItems
}

func formatItemTitle(number int, title string) string {
	r := []rune(title)
	if len(r) > 50 {
		title = string(r[:49]) + "…"
	}
	return fmt.Sprintf("#%d %s", number, title)
}

func formatAge(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

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
	Runs   []RunInfo
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

func toRunItems(runs []RunInfo) []state.ConnectorItem {
	items := make([]state.ConnectorItem, len(runs))
	for i, r := range runs {
		symbol := "◌"
		switch {
		case r.Status == "completed" && r.Conclusion == "failure":
			symbol = "✗"
		case r.Status == "in_progress":
			symbol = "▶"
		case r.Status == "queued" || r.Status == "waiting" || r.Status == "pending":
			symbol = "◌"
		}
		title := truncate(r.Name, 50)
		items[i] = state.ConnectorItem{
			Symbol: symbol,
			Title:  title,
			Meta:   r.Repo + " · " + r.Branch + " · " + formatAge(r.Age),
		}
	}
	return items
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

func formatItemTitle(number int, title string) string {
	return fmt.Sprintf("#%d %s", number, truncate(title, 50))
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

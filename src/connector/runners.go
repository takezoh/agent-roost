package connector

import (
	"github.com/takezoh/agent-roost/lib/github"
	"github.com/takezoh/agent-roost/runtime/worker"
)

func RegisterRunners() {
	worker.RegisterRunner("github_fetch", newGitHubFetch())
}

func newGitHubFetch() func(GitHubFetchInput) (GitHubFetchResult, error) {
	return func(_ GitHubFetchInput) (GitHubFetchResult, error) {
		summary, err := github.FetchSummary()
		if err != nil {
			return GitHubFetchResult{}, err
		}
		prs := make([]PRInfo, len(summary.PRs))
		for i, p := range summary.PRs {
			prs[i] = PRInfo{
				Number: p.Number, Title: p.Title,
				Repo: p.Repo, URL: p.URL, Age: p.Age,
			}
		}
		issues := make([]IssueInfo, len(summary.Issues))
		for i, iss := range summary.Issues {
			issues[i] = IssueInfo{
				Number: iss.Number, Title: iss.Title,
				Repo: iss.Repo, URL: iss.URL, Age: iss.Age,
			}
		}
		runs := make([]RunInfo, len(summary.Runs))
		for i, r := range summary.Runs {
			runs[i] = RunInfo{
				Name: r.Name, Status: r.Status, Conclusion: r.Conclusion,
				Branch: r.Branch, Repo: r.Repo, URL: r.URL, Age: r.Age,
			}
		}
		return GitHubFetchResult{PRs: prs, Issues: issues, Runs: runs}, nil
	}
}

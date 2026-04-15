package github

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"sync"
	"time"
)

var (
	ErrNotAvailable = errors.New("gh CLI not available")

	ghOnce  sync.Once
	ghFound bool
)

type Summary struct {
	PRs    []Item
	Issues []Item
	Runs   []Run
}

type Run struct {
	Name       string
	Status     string
	Conclusion string
	Branch     string
	Repo       string
	URL        string
	Age        time.Duration
}

type Item struct {
	Number int
	Title  string
	Repo   string
	URL    string
	Age    time.Duration
}

type ghSearchItem struct {
	Number     int    `json:"number"`
	Title      string `json:"title"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	URL       string    `json:"url"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func FetchSummary(ctx context.Context) (Summary, error) {
	ghOnce.Do(func() { _, err := exec.LookPath("gh"); ghFound = err == nil })
	if !ghFound {
		return Summary{}, ErrNotAvailable
	}

	prs, err := searchPRs(ctx)
	if err != nil {
		return Summary{}, err
	}
	issues, err := searchIssues(ctx)
	if err != nil {
		return Summary{}, err
	}
	runs, _ := fetchRuns(ctx)
	return Summary{PRs: prs, Issues: issues, Runs: runs}, nil
}

func searchPRs(ctx context.Context) ([]Item, error) {
	out, err := exec.CommandContext(ctx, "gh", "search", "prs",
		"--author=@me", "--state=open",
		"--json", "number,title,repository,url,updatedAt",
	).Output()
	if err != nil {
		return nil, err
	}
	return parseItems(out)
}

func searchIssues(ctx context.Context) ([]Item, error) {
	owned, err := runIssueSearch(ctx, "--owner=@me")
	if err != nil {
		return nil, err
	}
	assigned, err := runIssueSearch(ctx, "--assignee=@me")
	if err != nil {
		return nil, err
	}
	return dedup(owned, assigned), nil
}

func runIssueSearch(ctx context.Context, filter string) ([]Item, error) {
	out, err := exec.CommandContext(ctx, "gh", "search", "issues",
		filter, "--state=open",
		"--json", "number,title,repository,url,updatedAt",
	).Output()
	if err != nil {
		return nil, err
	}
	return parseItems(out)
}

func dedup(primary, secondary []Item) []Item {
	seen := make(map[string]struct{}, len(primary))
	for _, item := range primary {
		seen[item.URL] = struct{}{}
	}
	result := make([]Item, len(primary), len(primary)+len(secondary))
	copy(result, primary)
	for _, item := range secondary {
		if _, ok := seen[item.URL]; !ok {
			result = append(result, item)
		}
	}
	return result
}

type ghRepo struct {
	NameWithOwner string `json:"nameWithOwner"`
}

type ghRunItem struct {
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	HeadBranch string    `json:"headBranch"`
	UpdatedAt  time.Time `json:"updatedAt"`
	URL        string    `json:"url"`
}

func fetchRuns(ctx context.Context) ([]Run, error) {
	repos, err := listMyRepos(ctx)
	if err != nil {
		return nil, err
	}

	type result struct {
		runs []Run
	}

	sem := make(chan struct{}, 5)
	ch := make(chan result, len(repos))
	for _, repo := range repos {
		go func(r string) {
			sem <- struct{}{}
			defer func() { <-sem }()
			runs, _ := listRepoRuns(ctx, r)
			ch <- result{runs: runs}
		}(repo)
	}

	var all []Run
	for range repos {
		res := <-ch
		all = append(all, res.runs...)
	}
	return all, nil
}

func listMyRepos(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "gh", "repo", "list", "--json", "nameWithOwner", "--limit", "30").Output()
	if err != nil {
		return nil, err
	}
	var repos []ghRepo
	if err := json.Unmarshal(out, &repos); err != nil {
		return nil, err
	}
	names := make([]string, len(repos))
	for i, r := range repos {
		names[i] = r.NameWithOwner
	}
	return names, nil
}

func listRepoRuns(ctx context.Context, repo string) ([]Run, error) {
	out, err := exec.CommandContext(ctx, "gh", "run", "list",
		"--repo", repo,
		"--json", "name,status,conclusion,headBranch,updatedAt,url",
		"--limit", "5",
	).Output()
	if err != nil {
		return nil, err
	}
	var raw []ghRunItem
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	now := time.Now()
	var runs []Run
	for _, r := range raw {
		if r.Status == "completed" && r.Conclusion != "failure" {
			continue
		}
		runs = append(runs, Run{
			Name:       r.Name,
			Status:     r.Status,
			Conclusion: r.Conclusion,
			Branch:     r.HeadBranch,
			Repo:       repo,
			URL:        r.URL,
			Age:        now.Sub(r.UpdatedAt),
		})
	}
	return runs, nil
}

func parseItems(data []byte) ([]Item, error) {
	var raw []ghSearchItem
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	items := make([]Item, len(raw))
	now := time.Now()
	for i, r := range raw {
		items[i] = Item{
			Number: r.Number,
			Title:  r.Title,
			Repo:   r.Repository.NameWithOwner,
			URL:    r.URL,
			Age:    now.Sub(r.UpdatedAt),
		}
	}
	return items, nil
}

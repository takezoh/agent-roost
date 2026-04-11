package github

import (
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
}

type Item struct {
	Number int
	Title  string
	Repo   string
	URL    string
	Age    time.Duration
}

type ghSearchItem struct {
	Number     int       `json:"number"`
	Title      string    `json:"title"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	URL       string    `json:"url"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func FetchSummary() (Summary, error) {
	ghOnce.Do(func() { _, err := exec.LookPath("gh"); ghFound = err == nil })
	if !ghFound {
		return Summary{}, ErrNotAvailable
	}

	prs, err := searchPRs()
	if err != nil {
		return Summary{}, err
	}
	issues, err := searchIssues()
	if err != nil {
		return Summary{}, err
	}
	return Summary{PRs: prs, Issues: issues}, nil
}

func searchPRs() ([]Item, error) {
	out, err := exec.Command("gh", "search", "prs",
		"--author=@me", "--state=open",
		"--json", "number,title,repository,url,updatedAt",
	).Output()
	if err != nil {
		return nil, err
	}
	return parseItems(out)
}

func searchIssues() ([]Item, error) {
	out, err := exec.Command("gh", "search", "issues",
		"--assignee=@me", "--state=open",
		"--json", "number,title,repository,url,updatedAt",
	).Output()
	if err != nil {
		return nil, err
	}
	return parseItems(out)
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

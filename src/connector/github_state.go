package connector

import (
	"time"

	"github.com/takezoh/agent-roost/state"
)

type PRInfo struct {
	Number int
	Title  string
	Repo   string
	URL    string
	Age    time.Duration
}

type IssueInfo struct {
	Number int
	Title  string
	Repo   string
	URL    string
	Age    time.Duration
}

type GitHubState struct {
	state.ConnectorStateBase
	PRs       []PRInfo
	Issues    []IssueInfo
	FetchedAt time.Time
	Available bool
	Fetching  bool
}

var _ state.ConnectorState = GitHubState{}

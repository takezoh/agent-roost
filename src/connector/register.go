package connector

import (
	"sync"

	"github.com/takezoh/agent-roost/state"
)

var registerOnce sync.Once

func RegisterDefaults() {
	registerOnce.Do(func() {
		state.RegisterConnector(GitHubConnector{})
	})
}

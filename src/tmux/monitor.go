package tmux

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"time"

	"github.com/take/agent-roost/session"
)

var (
	promptPattern = regexp.MustCompile(`(?m)(^>|[>$❯]\s*$)`)
	costPattern   = regexp.MustCompile(`\$[\d.]+`)
)

type Monitor struct {
	client        *Client
	idleThreshold time.Duration
	lastContent   map[string]string
	lastActivity  map[string]time.Time
}

func NewMonitor(client *Client, idleThresholdSec int) *Monitor {
	return &Monitor{
		client:        client,
		idleThreshold: time.Duration(idleThresholdSec) * time.Second,
		lastContent:   make(map[string]string),
		lastActivity:  make(map[string]time.Time),
	}
}

func (m *Monitor) PollAll(windowIDs []string) map[string]session.State {
	states := make(map[string]session.State, len(windowIDs))
	for _, id := range windowIDs {
		states[id] = m.DetectState(id)
	}
	return states
}

func (m *Monitor) DetectState(windowID string) session.State {
	content, err := m.client.CapturePaneLines(windowID+".0", 5)
	if err != nil {
		return session.StateStopped
	}

	hash := hashContent(content)
	prev, seen := m.lastContent[windowID]

	if !seen || hash != prev {
		m.lastContent[windowID] = hash
		m.lastActivity[windowID] = time.Now()
		if hasPromptIndicator(content) {
			return session.StateWaiting
		}
		return session.StateRunning
	}

	if time.Since(m.lastActivity[windowID]) > m.idleThreshold {
		return session.StateIdle
	}
	return session.StateWaiting
}

func (m *Monitor) ExtractCost(windowID string) string {
	content, err := m.client.CapturePaneLines(windowID+".0", 2)
	if err != nil {
		return ""
	}
	return costPattern.FindString(content)
}

func hashContent(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func hasPromptIndicator(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimRight(line, " ")
		if trimmed == "" {
			continue
		}
		if promptPattern.MatchString(trimmed) {
			return true
		}
	}
	return false
}

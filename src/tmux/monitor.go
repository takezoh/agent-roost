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

type snapshot struct {
	hash         string
	lastActivity time.Time
	lastState    session.State
}

type Monitor struct {
	capturer      PaneCapturer
	idleThreshold time.Duration
	snapshots     map[string]snapshot
}

func NewMonitor(capturer PaneCapturer, idleThresholdSec int) *Monitor {
	return &Monitor{
		capturer:      capturer,
		idleThreshold: time.Duration(idleThresholdSec) * time.Second,
		snapshots:     make(map[string]snapshot),
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
	content, err := m.capturer.CapturePaneLines(windowID+".0", 5)
	if err != nil {
		return session.StateStopped
	}
	state, snap := computeTransition(content, m.snapshots[windowID], time.Now(), m.idleThreshold)
	m.snapshots[windowID] = snap
	return state
}

func computeTransition(content string, prev snapshot, now time.Time, threshold time.Duration) (session.State, snapshot) {
	hash := hashContent(content)
	if prev.hash == "" || hash != prev.hash {
		state := session.StateRunning
		if hasPromptIndicator(content) {
			state = session.StateWaiting
		}
		return state, snapshot{hash: hash, lastActivity: now, lastState: state}
	}
	if now.Sub(prev.lastActivity) > threshold {
		return session.StateIdle, snapshot{hash: prev.hash, lastActivity: prev.lastActivity, lastState: session.StateIdle}
	}
	return prev.lastState, prev
}

func (m *Monitor) ExtractCost(windowID string) string {
	content, err := m.capturer.CapturePaneLines(windowID+".0", 2)
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

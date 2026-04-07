package tmux

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
)

var defaultPromptPattern = regexp.MustCompile(`(?m)(^>|[>$❯]\s*$)`)

var (
	costPattern = regexp.MustCompile(`\$[\d.]+`)
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
	registry      *driver.Registry
}

// NewMonitor initializes a Monitor.
// When registry is nil, the default generic pattern is used.
func NewMonitor(capturer PaneCapturer, idleThresholdSec int, registry *driver.Registry) *Monitor {
	return &Monitor{
		capturer:      capturer,
		idleThreshold: time.Duration(idleThresholdSec) * time.Second,
		snapshots:     make(map[string]snapshot),
		registry:      registry,
	}
}

// PollAll takes a windowID-to-command map and returns the state of each window.
func (m *Monitor) PollAll(windowCommands map[string]string) map[string]session.State {
	states := make(map[string]session.State, len(windowCommands))
	for id, cmd := range windowCommands {
		states[id] = m.DetectState(id, cmd)
	}
	return states
}

// DetectState detects the current state of the specified window.
func (m *Monitor) DetectState(windowID, command string) session.State {
	content, err := m.capturer.CapturePaneLines(windowID+".0", 5)
	if err != nil {
		return session.StateStopped
	}
	pattern := m.patternFor(command)
	prev := m.snapshots[windowID]
	state, snap := computeTransition(content, prev, time.Now(), m.idleThreshold, pattern)
	if prev.lastState != state {
		slog.Info("state changed", "window", windowID, "from", prev.lastState, "to", state)
	}
	m.snapshots[windowID] = snap
	return state
}

func (m *Monitor) patternFor(command string) *regexp.Regexp {
	if m.registry != nil {
		return m.registry.CompiledPattern(command)
	}
	return defaultPromptPattern
}

func computeTransition(content string, prev snapshot, now time.Time, threshold time.Duration, pattern *regexp.Regexp) (session.State, snapshot) {
	if pattern == nil {
		pattern = defaultPromptPattern
	}
	hash := hashContent(content)
	if prev.hash == "" || hash != prev.hash {
		state := session.StateRunning
		if hasPromptIndicator(content, pattern) {
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

func hasPromptIndicator(content string, pattern *regexp.Regexp) bool {
	if pattern == nil {
		pattern = defaultPromptPattern
	}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimRight(line, " ")
		if trimmed == "" {
			continue
		}
		if pattern.MatchString(trimmed) {
			return true
		}
	}
	return false
}

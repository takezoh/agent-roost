package driver

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// Shared helpers for polling-driven observers (genericObserver, etc.).
// Centralized so we don't duplicate the hash + prompt-detection algorithm
// across every driver that uses capture-pane heuristics.

func hashContent(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func hasPromptIndicator(content string, pattern *regexp.Regexp) bool {
	if pattern == nil {
		return false
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

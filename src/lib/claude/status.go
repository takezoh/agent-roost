package claude

import (
	"encoding/json"
	"fmt"
	"strings"
)

// StatusData holds parsed status line data from Claude Code.
type StatusData struct {
	SessionID    string
	Cost         float64
	ContextUsed  int
	Model        string
	InputTokens  int
	OutputTokens int
}

// ParseStatusLine parses Claude Code's status line JSON.
func ParseStatusLine(data []byte) (StatusData, error) {
	var raw struct {
		SessionID string `json:"session_id"`
		Model     any    `json:"model"`
		Cost      struct {
			TotalCostUSD float64 `json:"total_cost_usd"`
		} `json:"cost"`
		ContextWindow struct {
			UsedPercentage    int `json:"used_percentage"`
			TotalInputTokens  int `json:"total_input_tokens"`
			TotalOutputTokens int `json:"total_output_tokens"`
		} `json:"context_window"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return StatusData{}, err
	}
	return StatusData{
		SessionID:    raw.SessionID,
		Cost:         raw.Cost.TotalCostUSD,
		ContextUsed:  raw.ContextWindow.UsedPercentage,
		Model:        shortenModel(raw.Model),
		InputTokens:  raw.ContextWindow.TotalInputTokens,
		OutputTokens: raw.ContextWindow.TotalOutputTokens,
	}, nil
}

// FormatStatusLine formats status data for tmux status bar display.
func (s StatusData) FormatStatusLine() string {
	var parts []string
	if s.Model != "" {
		parts = append(parts, s.Model)
	}
	if s.ContextUsed > 0 {
		filled := s.ContextUsed / 10
		bar := strings.Repeat("█", filled) + strings.Repeat("░", 10-filled)
		parts = append(parts, fmt.Sprintf("ctx:%d%% %s", s.ContextUsed, bar))
	}
	if s.Cost >= 0.01 {
		parts = append(parts, fmt.Sprintf("$%.2f", s.Cost))
	}
	if s.InputTokens > 0 || s.OutputTokens > 0 {
		parts = append(parts, fmt.Sprintf("%s↓ %s↑", formatTokens(s.InputTokens), formatTokens(s.OutputTokens)))
	}
	return strings.Join(parts, " | ")
}

func shortenModel(v any) string {
	var id string
	switch m := v.(type) {
	case map[string]any:
		id, _ = m["id"].(string)
	case string:
		id = m
	}
	if id == "" {
		return ""
	}
	id = strings.TrimPrefix(id, "claude-")
	if i := strings.LastIndex(id, "-20"); i > 0 {
		id = id[:i]
	}
	return id
}

func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%dk", n/1000)
}

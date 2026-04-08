package claude

import (
	"encoding/json"
	"fmt"
	"strings"
)

// TurnUsage holds per-turn usage data extracted from a transcript assistant entry.
type TurnUsage struct {
	Model                    string
	InputTokens              int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	OutputTokens             int
}

// TotalInputTokens returns the sum of all input token types.
func (u TurnUsage) TotalInputTokens() int {
	return u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
}

// ParseTurnUsage parses a single transcript JSONL line and extracts usage data.
// Returns nil if the line is not an assistant entry or has no usage data.
func ParseTurnUsage(line []byte) *TurnUsage {
	var entry struct {
		Type    string `json:"type"`
		Message *struct {
			Model any `json:"model"`
			Usage *struct {
				InputTokens              int `json:"input_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
				OutputTokens             int `json:"output_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &entry) != nil {
		return nil
	}
	if entry.Type != "assistant" || entry.Message == nil || entry.Message.Usage == nil {
		return nil
	}
	u := entry.Message.Usage
	return &TurnUsage{
		Model:                    shortenModel(entry.Message.Model),
		InputTokens:              u.InputTokens,
		CacheCreationInputTokens: u.CacheCreationInputTokens,
		CacheReadInputTokens:     u.CacheReadInputTokens,
		OutputTokens:             u.OutputTokens,
	}
}

// FormatUsageStatusLine formats model and token counts for tmux status bar display.
func FormatUsageStatusLine(model string, inputTokens, outputTokens int) string {
	var parts []string
	if model != "" {
		parts = append(parts, model)
	}
	if inputTokens > 0 || outputTokens > 0 {
		parts = append(parts, fmt.Sprintf("%s↓ %s↑", formatTokens(inputTokens), formatTokens(outputTokens)))
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

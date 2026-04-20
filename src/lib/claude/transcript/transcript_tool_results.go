package transcript

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseToolUseResult decodes the top-level toolUseResult field of a
// Claude transcript user line into a tool-specific ToolResult
// implementation. Unknown tools and empty payloads return a
// GenericResult (char count fallback).
func ParseToolUseResult(name string, raw json.RawMessage) ToolResult {
	if len(raw) == 0 {
		return nil
	}
	switch name {
	case "Bash":
		return parseBashResult(raw)
	case "Edit", "Write", "MultiEdit":
		return parseEditResult(raw)
	case "Read":
		return parseReadResult(raw)
	case "Glob", "Grep":
		return parseGlobGrepResult(raw)
	case "Agent", "Task":
		return parseAgentResult(raw)
	case "TodoWrite":
		return parseTodoResult(raw)
	case "WebFetch", "WebSearch":
		return parseWebResult(name, raw)
	case "ExitPlanMode":
		return parseExitPlanResult(raw)
	}
	return GenericResult{Chars: genericChars(raw)}
}

// --- Glob / Grep ---

type GlobGrepResult struct {
	NumFiles int
	NumLines int
	Mode     string
}

func (GlobGrepResult) isToolResult() {}

func (r GlobGrepResult) Summary() string {
	if r.NumFiles == 0 && r.NumLines == 0 {
		return "no matches"
	}
	if r.NumLines > 0 {
		return fmt.Sprintf("%d files, %d hits", r.NumFiles, r.NumLines)
	}
	return fmt.Sprintf("%d files", r.NumFiles)
}

func parseGlobGrepResult(raw json.RawMessage) GlobGrepResult {
	var v struct {
		NumFiles int    `json:"numFiles"`
		NumLines int    `json:"numLines"`
		Mode     string `json:"mode"`
	}
	_ = json.Unmarshal(raw, &v)
	return GlobGrepResult{
		NumFiles: v.NumFiles,
		NumLines: v.NumLines,
		Mode:     v.Mode,
	}
}

// --- Agent / Task ---

type AgentResult struct {
	AgentID    string
	AgentType  string
	Status     string
	DurationMs int
	Tokens     int
}

func (AgentResult) isToolResult() {}

func (r AgentResult) Summary() string {
	label := r.Status
	if label == "" {
		label = "done"
	}
	if r.AgentType != "" {
		label = fmt.Sprintf("[%s] %s", r.AgentType, label)
	}
	if r.DurationMs > 0 {
		label += fmt.Sprintf(" %.1fs", float64(r.DurationMs)/1000.0)
	}
	if r.Tokens > 0 {
		label += fmt.Sprintf(" %s tok", humanThousands(r.Tokens))
	}
	return label
}

func parseAgentResult(raw json.RawMessage) AgentResult {
	var v struct {
		AgentID     string `json:"agentId"`
		AgentType   string `json:"agentType"`
		Status      string `json:"status"`
		TotalMs     int    `json:"totalDurationMs"`
		TotalTokens int    `json:"totalTokens"`
	}
	_ = json.Unmarshal(raw, &v)
	return AgentResult{
		AgentID:    v.AgentID,
		AgentType:  v.AgentType,
		Status:     v.Status,
		DurationMs: v.TotalMs,
		Tokens:     v.TotalTokens,
	}
}

// --- TodoWrite ---

type TodoResult struct {
	Text string
}

func (TodoResult) isToolResult() {}
func (r TodoResult) Summary() string {
	if r.Text == "" {
		return "updated"
	}
	return r.Text
}

func parseTodoResult(raw json.RawMessage) TodoResult {
	var v struct {
		NewTodos []struct {
			Status string `json:"status"`
		} `json:"newTodos"`
	}
	_ = json.Unmarshal(raw, &v)
	if len(v.NewTodos) == 0 {
		return TodoResult{}
	}
	counts := map[string]int{}
	for _, t := range v.NewTodos {
		counts[t.Status]++
	}
	var parts []string
	for _, s := range []string{"pending", "in_progress", "completed"} {
		if counts[s] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[s], s))
		}
	}
	if len(parts) == 0 {
		return TodoResult{Text: fmt.Sprintf("%d todos", len(v.NewTodos))}
	}
	return TodoResult{Text: strings.Join(parts, ", ")}
}

// --- Web ---

type WebResult struct {
	URL   string
	Chars int
	Query string
}

func (WebResult) isToolResult() {}
func (r WebResult) Summary() string {
	switch {
	case r.URL != "" && r.Chars > 0:
		return fmt.Sprintf("%d chars", r.Chars)
	case r.Query != "":
		return r.Query
	}
	return "fetched"
}

func parseWebResult(_ string, raw json.RawMessage) WebResult {
	var v struct {
		URL     string `json:"url"`
		Query   string `json:"query"`
		Content string `json:"content"`
		Result  string `json:"result"`
	}
	_ = json.Unmarshal(raw, &v)
	body := v.Content
	if body == "" {
		body = v.Result
	}
	return WebResult{URL: v.URL, Query: v.Query, Chars: len(body)}
}

// --- ExitPlanMode ---

type ExitPlanResult struct {
	PlanHead string
}

func (ExitPlanResult) isToolResult() {}
func (r ExitPlanResult) Summary() string {
	if r.PlanHead == "" {
		return "plan submitted"
	}
	return r.PlanHead
}

func parseExitPlanResult(raw json.RawMessage) ExitPlanResult {
	var v struct {
		Plan string `json:"plan"`
	}
	_ = json.Unmarshal(raw, &v)
	return ExitPlanResult{PlanHead: firstLine(v.Plan)}
}

// --- helpers ---

func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

func leadingLines(s string, maxLines, maxLen int) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	out := make([]string, 0, maxLines)
	for _, l := range lines {
		l = strings.TrimRight(l, "\r")
		if l == "" {
			continue
		}
		if len([]rune(l)) > maxLen {
			l = string([]rune(l)[:maxLen]) + "…"
		}
		out = append(out, l)
		if len(out) >= maxLines {
			break
		}
	}
	return out
}

func genericChars(raw json.RawMessage) int {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return len(s)
	}
	return len(raw)
}

func humanThousands(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

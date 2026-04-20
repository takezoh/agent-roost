package transcript

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseToolInput extracts a human-readable ToolInput summary from a
// tool_use input. Unknown tools yield a zero-value ToolInput.
func ParseToolInput(name string, input json.RawMessage) ToolInput {
	if len(input) == 0 {
		return ToolInput{}
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(input, &m) != nil {
		return ToolInput{}
	}
	g := fieldGetter(m)
	if strings.HasPrefix(name, "mcp__") {
		return mcpToolInput(name, g)
	}
	if h, ok := toolInputHandlers[name]; ok {
		return h(g)
	}
	return ToolInput{}
}

type toolInputFunc func(fieldGetter) ToolInput

var toolInputHandlers = map[string]toolInputFunc{
	"Read":            filePathInput,
	"Write":           filePathInput,
	"Edit":            filePathInput,
	"MultiEdit":       multiEditInput,
	"NotebookEdit":    notebookInput,
	"Glob":            patternPathInput,
	"Grep":            patternPathInput,
	"Bash":            bashInput,
	"WebFetch":        webFetchInput,
	"WebSearch":       webSearchInput,
	"Agent":           agentInput,
	"Task":            agentInput,
	"TaskCreate":      taskCreateInput,
	"TaskUpdate":      taskUpdateInput,
	"TaskGet":         taskIDInput,
	"TaskList":        taskIDInput,
	"TaskOutput":      taskIDInput,
	"TaskStop":        taskIDInput,
	"TodoWrite":       todoWriteInput,
	"ExitPlanMode":    exitPlanInput,
	"AskUserQuestion": askQuestionInput,
}

func filePathInput(g fieldGetter) ToolInput {
	return ToolInput{Primary: g.str("file_path")}
}

func multiEditInput(g fieldGetter) ToolInput {
	return ToolInput{Primary: g.str("file_path"), Detail: plural(g.rawLen("edits"), "edit")}
}

func notebookInput(g fieldGetter) ToolInput {
	return ToolInput{Primary: g.str("notebook_path"), Detail: g.str("cell_id")}
}

func patternPathInput(g fieldGetter) ToolInput {
	return ToolInput{Primary: g.str("pattern"), Detail: g.str("path")}
}

func bashInput(g fieldGetter) ToolInput {
	return ToolInput{Primary: g.str("command"), Detail: g.str("description")}
}

func webFetchInput(g fieldGetter) ToolInput {
	return ToolInput{Primary: g.str("url"), Detail: g.str("prompt")}
}

func webSearchInput(g fieldGetter) ToolInput {
	return ToolInput{Primary: g.str("query")}
}

func agentInput(g fieldGetter) ToolInput {
	primary := g.str("description")
	if primary == "" {
		primary = g.str("subject")
	}
	return ToolInput{Primary: primary, Detail: g.str("subagent_type")}
}

func taskCreateInput(g fieldGetter) ToolInput {
	return ToolInput{Primary: g.str("subject")}
}

func taskUpdateInput(g fieldGetter) ToolInput {
	return ToolInput{Primary: g.str("taskId"), Detail: g.str("status")}
}

func taskIDInput(g fieldGetter) ToolInput {
	return ToolInput{Primary: g.str("taskId")}
}

func todoWriteInput(g fieldGetter) ToolInput {
	return ToolInput{Primary: summarizeTodos(g.raw("todos"))}
}

func exitPlanInput(g fieldGetter) ToolInput {
	plan := firstLine(g.str("plan"))
	if plan == "" {
		plan = "(plan)"
	}
	return ToolInput{Primary: plan}
}

func askQuestionInput(g fieldGetter) ToolInput {
	return ToolInput{Primary: firstQuestion(g.raw("questions"))}
}

// fieldGetter wraps a decoded RawMessage map with typed accessors.
type fieldGetter map[string]json.RawMessage

func (g fieldGetter) raw(key string) json.RawMessage {
	return g[key]
}

func (g fieldGetter) str(key string) string {
	raw, ok := g[key]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

func (g fieldGetter) rawLen(key string) int {
	raw, ok := g[key]
	if !ok {
		return 0
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil {
		return len(arr)
	}
	return 0
}

func mcpToolInput(name string, g fieldGetter) ToolInput {
	// Try a handful of conventional keys commonly used by MCP tools.
	keys := []string{"path", "url", "uri", "query", "id", "name", "command"}
	for _, k := range keys {
		if v := g.str(k); v != "" {
			return ToolInput{Primary: v, Detail: shortenMcpName(name)}
		}
	}
	// Fall back to the first scalar field we can find.
	for k, raw := range g {
		var s string
		if json.Unmarshal(raw, &s) == nil && s != "" {
			return ToolInput{Primary: s, Detail: k}
		}
	}
	return ToolInput{Detail: shortenMcpName(name)}
}

// shortenMcpName turns "mcp__filesystem__read_text_file" into "filesystem".
func shortenMcpName(name string) string {
	parts := strings.SplitN(strings.TrimPrefix(name, "mcp__"), "__", 2)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func summarizeTodos(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var todos []struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(raw, &todos) != nil {
		return ""
	}
	if len(todos) == 0 {
		return ""
	}
	counts := map[string]int{}
	for _, t := range todos {
		counts[t.Status]++
	}
	var parts []string
	for _, s := range []string{"pending", "in_progress", "completed"} {
		if counts[s] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[s], s))
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%d todos", len(todos))
	}
	return strings.Join(parts, ", ")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func firstQuestion(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var qs []struct {
		Question string `json:"question"`
	}
	if json.Unmarshal(raw, &qs) != nil || len(qs) == 0 {
		return ""
	}
	return qs[0].Question
}

func plural(n int, noun string) string {
	if n == 0 {
		return ""
	}
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

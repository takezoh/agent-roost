package transcript

import "fmt"

// Kind is the category of a parsed transcript entry.
type Kind int

const (
	KindUser Kind = iota
	KindAssistantText
	KindAssistantThinking
	KindToolUse
	KindToolResult
	KindSystem
	KindAttachment
	KindFileSnapshot
	KindCustomTitle
	KindAgentName
	KindSubagentBegin
	KindSubagentEnd
	KindUnknown
)

// Entry is a single structured record parsed from one Claude transcript line.
// PR1 carries the minimal fields required to reproduce the legacy rendering
// behaviour; richer fields (UUID, Timestamp, etc.) land in later PRs.
type Entry struct {
	Kind       Kind
	Depth      int
	Text       string
	ToolName   string
	ToolUseID  string
	ToolInput  ToolInput
	ToolResult ToolResult
	IsError    bool
}

// ToolInput is a human-readable summary of a tool_use input.
// Primary holds the most salient parameter (file path, command, URL,
// query, ...) and Detail holds an optional secondary hint shown in
// parentheses when present.
type ToolInput struct {
	Primary string
	Detail  string
}

// ToolResult is the structured summary of a tool_result payload.
// PR1 ships GenericResult only; per-tool implementations arrive in PR2.
type ToolResult interface {
	isToolResult()
	Summary() string
}

// GenericResult is the fallback summary used when no tool-specific parser
// matches. It mirrors the legacy "(N chars)" rendering.
type GenericResult struct {
	Chars int
}

func (GenericResult) isToolResult() {}

func (r GenericResult) Summary() string {
	if r.Chars == 0 {
		return "ok"
	}
	return fmt.Sprintf("(%d chars)", r.Chars)
}

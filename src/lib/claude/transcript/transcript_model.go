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
	KindLastPrompt
	KindSubagentBegin
	KindSubagentEnd
	KindUnknown
)

// Entry is a single structured record parsed from one Claude transcript line.
// UUID / ParentUUID are populated from the JSONL line wrapper for any
// "user" or "assistant" entry; meta entries (custom-title, system, etc.)
// have no uuid in the wire format and leave both fields empty.
//
// Synthetic marks KindUser entries that originate from Claude-injected
// content blocks rather than the user's CLI input. Examples are skill
// bootstrap text ("Base directory for this skill: ..."), interrupt
// markers ("[Request interrupted by user]"), and command output echoes.
// transcript.Tracker uses this flag to keep these out of the lastPrompt
// chain while still letting renderers display them.
type Entry struct {
	Kind       Kind
	Depth      int
	Text       string
	ToolName   string
	ToolUseID  string
	ToolInput  ToolInput
	ToolResult ToolResult
	IsError    bool
	UUID       string
	ParentUUID string
	Synthetic  bool
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

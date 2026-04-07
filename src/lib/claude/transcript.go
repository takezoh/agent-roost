package claude

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	userStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00bfff"))
	toolStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	toolErrStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff6666"))
)

// FormatTranscript parses JSONL lines and returns styled text for display.
func FormatTranscript(raw string) string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		for _, e := range parseTranscriptLine(line) {
			out = append(out, e.render())
		}
	}
	return strings.Join(out, "\n")
}

type entryKind int

const (
	entryUser entryKind = iota
	entryAssistant
	entryToolUse
	entryToolResult
)

type transcriptEntry struct {
	kind    entryKind
	text    string
	isError bool
}

func (e transcriptEntry) render() string {
	switch e.kind {
	case entryUser:
		return userStyle.Render("YOU> " + e.text)
	case entryAssistant:
		return e.text
	case entryToolUse:
		return toolStyle.Render("  ▸ " + e.text)
	case entryToolResult:
		if e.isError {
			return toolErrStyle.Render("  " + e.text)
		}
		return toolStyle.Render("  " + e.text)
	default:
		return ""
	}
}

func parseTranscriptLine(line string) []transcriptEntry {
	var entry struct {
		Type    string       `json:"type"`
		Message *jsonMessage `json:"message,omitempty"`
	}
	if json.Unmarshal([]byte(line), &entry) != nil {
		return nil
	}
	switch entry.Type {
	case "user":
		return parseUserEntry(entry.Message)
	case "assistant":
		return parseAssistantEntry(entry.Message)
	default:
		return nil
	}
}

type jsonMessage struct {
	Content json.RawMessage `json:"content"`
}

func parseUserEntry(msg *jsonMessage) []transcriptEntry {
	if msg == nil {
		return nil
	}
	var s string
	if json.Unmarshal(msg.Content, &s) == nil {
		s = stripSystemTags(strings.TrimSpace(s))
		if s == "" {
			return nil
		}
		return []transcriptEntry{{kind: entryUser, text: s}}
	}
	var blocks []struct {
		Type    string          `json:"type"`
		Text    string          `json:"text"`
		Content json.RawMessage `json:"content"`
		IsError bool            `json:"is_error"`
	}
	if json.Unmarshal(msg.Content, &blocks) != nil {
		return nil
	}
	var entries []transcriptEntry
	for _, b := range blocks {
		switch b.Type {
		case "text":
			t := strings.TrimSpace(b.Text)
			if t != "" {
				entries = append(entries, transcriptEntry{kind: entryUser, text: t})
			}
		case "tool_result":
			entries = append(entries, makeToolResult(b.Content, b.IsError))
		}
	}
	return entries
}

func parseAssistantEntry(msg *jsonMessage) []transcriptEntry {
	if msg == nil {
		return nil
	}
	var blocks []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if json.Unmarshal(msg.Content, &blocks) != nil {
		return nil
	}
	var entries []transcriptEntry
	for _, b := range blocks {
		switch b.Type {
		case "text":
			t := strings.TrimSpace(b.Text)
			if t != "" {
				entries = append(entries, transcriptEntry{kind: entryAssistant, text: t})
			}
		case "tool_use":
			entries = append(entries, transcriptEntry{
				kind: entryToolUse,
				text: formatToolUse(b.Name, b.Input),
			})
		}
	}
	return entries
}

func formatToolUse(name string, input json.RawMessage) string {
	param := extractToolParam(name, input)
	if param == "" {
		return name
	}
	return name + "  " + param
}

const maxToolParamLen = 80

func extractToolParam(name string, input json.RawMessage) string {
	var key string
	switch name {
	case "Read", "Write", "Edit":
		key = "file_path"
	case "Glob":
		key = "pattern"
	case "Bash":
		key = "command"
	case "Grep":
		key = "pattern"
	case "Agent":
		key = "description"
	default:
		return ""
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(input, &m) != nil {
		return ""
	}
	raw, ok := m[key]
	if !ok {
		return ""
	}
	var val string
	if json.Unmarshal(raw, &val) != nil {
		return ""
	}
	return truncate(val, maxToolParamLen)
}

func makeToolResult(content json.RawMessage, isError bool) transcriptEntry {
	n := contentLen(content)
	var text string
	if isError {
		if n == 0 {
			text = "← error"
		} else {
			text = fmt.Sprintf("← error (%d chars)", n)
		}
	} else {
		if n == 0 {
			text = "← ok"
		} else {
			text = fmt.Sprintf("← (%d chars)", n)
		}
	}
	return transcriptEntry{kind: entryToolResult, text: text, isError: isError}
}

func contentLen(raw json.RawMessage) int {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return len(s)
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		n := 0
		for _, b := range blocks {
			n += len(b.Text)
		}
		return n
	}
	return 0
}

var systemTagPrefixes = []string{
	"<local-command-caveat>",
	"<command-name>",
	"<system-reminder>",
}

func stripSystemTags(s string) string {
	for _, prefix := range systemTagPrefixes {
		if strings.HasPrefix(s, prefix) {
			return ""
		}
	}
	return s
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

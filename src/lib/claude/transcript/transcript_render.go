package transcript

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	userStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00bfff"))
	toolStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	toolErrStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff6666"))
	toolDimStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
	thinkingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#777777")).Italic(true)
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffaa00"))
	agentStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#9966ff"))
	depthDimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#444444"))
)

const (
	maxToolPrimaryLen   = 120
	toolUsePrefix       = "  ▸ "
	toolResultPrefix    = "    ← "
	toolDetailPrefix    = "      "
	thinkingPrefix      = "  ⋯ "
	systemPrefix        = "  § "
	attachmentPrefix    = "  ⊕ "
	titlePrefix         = "  # "
	agentNamePrefix     = "  @ "
	subagentBeginPrefix = "┌─ "
	subagentEndPrefix   = "└─"
	depthIndent         = "│ "
	maxThinkingLines    = 8
)

// RenderEntries joins the rendered form of each Entry with newlines.
// Empty renders are dropped so metadata-only lines do not leave blank rows.
func RenderEntries(entries []Entry) string {
	if len(entries) == 0 {
		return ""
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		s := e.Render()
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return strings.Join(out, "\n")
}

// Render returns the styled representation of an Entry. Rich tool results
// may include multiple newline-separated lines. Returns the empty string
// for entries that should not appear in the transcript view.
func (e Entry) Render() string {
	body := e.renderBody()
	if body == "" || e.Depth == 0 {
		return body
	}
	indent := strings.Repeat(depthIndent, e.Depth)
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		lines[i] = depthDimStyle.Render(indent) + l
	}
	return strings.Join(lines, "\n")
}

func (e Entry) renderBody() string {
	switch e.Kind {
	case KindUser:
		return userStyle.Render("YOU> " + e.Text)
	case KindAssistantText:
		return e.Text
	case KindAssistantThinking:
		return renderThinking(e.Text)
	case KindToolUse:
		return renderToolUse(e)
	case KindToolResult:
		return renderToolResult(e)
	case KindSystem:
		return toolDimStyle.Render(systemPrefix + e.Text)
	case KindAttachment:
		return toolDimStyle.Render(attachmentPrefix + e.Text)
	case KindCustomTitle:
		return titleStyle.Render(titlePrefix + e.Text)
	case KindAgentName:
		return agentStyle.Render(agentNamePrefix + e.Text)
	case KindSubagentBegin:
		return agentStyle.Render(subagentBeginPrefix + e.Text)
	case KindSubagentEnd:
		return agentStyle.Render(subagentEndPrefix)
	case KindFileSnapshot:
		return ""
	default:
		return ""
	}
}

func renderThinking(text string) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, maxThinkingLines+1)
	for i, l := range lines {
		l = strings.TrimRight(l, "\r")
		if i >= maxThinkingLines {
			rest := len(lines) - maxThinkingLines
			out = append(out, thinkingStyle.Render(thinkingPrefix+fmt.Sprintf("[+%d more lines]", rest)))
			break
		}
		out = append(out, thinkingStyle.Render(thinkingPrefix+l))
	}
	return strings.Join(out, "\n")
}

func renderToolUse(e Entry) string {
	head := toolUsePrefix + formatToolUseHead(e.ToolName, e.ToolInput)
	return toolStyle.Render(head)
}

func formatToolUseHead(name string, in ToolInput) string {
	primary := truncate(in.Primary, maxToolPrimaryLen)
	if primary == "" && in.Detail == "" {
		return name
	}
	if primary == "" {
		return name + "  (" + in.Detail + ")"
	}
	if in.Detail == "" {
		return name + "  " + primary
	}
	return fmt.Sprintf("%s  %s  (%s)", name, primary, in.Detail)
}

func renderToolResult(e Entry) string {
	summary := "ok"
	if e.ToolResult != nil {
		if s := e.ToolResult.Summary(); s != "" {
			summary = s
		}
	}
	head := toolResultPrefix
	if e.IsError {
		head += "error " + summary
	} else {
		head += summary
	}
	var lines []string
	style := toolStyle
	if e.IsError {
		style = toolErrStyle
	}
	lines = append(lines, style.Render(head))

	// Extra detail lines (stdout head, etc.).
	for _, detail := range toolResultDetails(e.ToolResult) {
		lines = append(lines, toolDimStyle.Render(toolDetailPrefix+detail))
	}
	return strings.Join(lines, "\n")
}

// toolResultDetails returns extra lines to show under the summary for
// result types that carry preview content (currently just Bash stdout
// head). Returning nil means the summary is shown alone.
func toolResultDetails(r ToolResult) []string {
	bash, ok := r.(BashResult)
	if !ok {
		return nil
	}
	return bash.StdoutHead
}

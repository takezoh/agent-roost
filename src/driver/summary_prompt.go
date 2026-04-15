package driver

import (
	"strings"
)

const (
	summaryEntryTextCap = 1500
	summaryTotalCap     = 12000
)

func appendHookPromptTurn(turns []SummaryTurn, hookPrompt string) []SummaryTurn {
	if hookPrompt == "" {
		return turns
	}
	if n := len(turns); n > 0 && turns[n-1].Role == "user" && turns[n-1].Text == hookPrompt {
		return turns
	}
	return append(turns, SummaryTurn{Role: "user", Text: hookPrompt})
}

func recentUserTurns(turns []SummaryTurn, userTurns int) []SummaryTurn {
	if userTurns <= 0 || len(turns) == 0 {
		return nil
	}
	start := 0
	seen := 0
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].Role == "user" {
			seen++
			if seen >= userTurns {
				start = i
				break
			}
		}
	}
	out := make([]SummaryTurn, len(turns)-start)
	copy(out, turns[start:])
	return out
}

func formatSummaryPrompt(prev string, turns []SummaryTurn) string {
	var b strings.Builder
	b.WriteString("You are a session summarizer. From the conversation history and previous summary below, ")
	b.WriteString("summarize what the user is currently trying to do in this AI coding session ")
	b.WriteString("into a 2-3 line descriptive message. ")
	b.WriteString("Each line covers a different perspective (goal / recent progress / next action) stated concisely, ")
	b.WriteString("with ~30 characters per line. ")
	b.WriteString("Return only the body text, no headings, decoration, preamble, or quotes.\n\n")
	if prev != "" {
		b.WriteString("<previous_summary>\n")
		b.WriteString(prev)
		b.WriteString("\n</previous_summary>\n\n")
	}
	b.WriteString("<recent_turns>\n")
	b.WriteString(renderRecentTurns(turns))
	b.WriteString("</recent_turns>\n")
	return b.String()
}

func renderRecentTurns(turns []SummaryTurn) string {
	clipped := make([]SummaryTurn, len(turns))
	for i, t := range turns {
		clipped[i] = SummaryTurn{Role: t.Role, Text: tailClip(t.Text, summaryEntryTextCap)}
	}
	var blocks []string
	prevRole := ""
	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		blocks = append(blocks, cur.String())
		cur.Reset()
	}
	for _, t := range clipped {
		if t.Role != prevRole {
			flush()
			cur.WriteString("[")
			cur.WriteString(t.Role)
			cur.WriteString("]\n")
			prevRole = t.Role
		} else {
			cur.WriteString("\n")
		}
		cur.WriteString(t.Text)
		cur.WriteString("\n")
	}
	flush()
	body := strings.Join(blocks, "\n")
	for len(body) > summaryTotalCap && len(blocks) > 1 {
		blocks = blocks[1:]
		body = strings.Join(blocks, "\n")
	}
	return body
}

func tailClip(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return "…" + string(r[len(r)-max:])
}

// formatGenericSummaryPrompt builds a summary prompt for non-agent terminal
// sessions (generic shells, git diff, tig, build logs, etc.). Unlike the
// agent-oriented formatSummaryPrompt which models the session as a
// user/assistant conversation, this prompt treats the capture as raw
// terminal screen contents, annotated with the session's command and
// working directory so the LLM can ground its interpretation.
func formatGenericSummaryPrompt(prev, command, workingDir, content string) string {
	var b strings.Builder
	b.WriteString("You are a terminal session summarizer. ")
	b.WriteString("Describe in 2-3 lines (roughly 30 characters each) what is being worked on ")
	b.WriteString("in this terminal session. ")
	b.WriteString("Ground your description in all three signals: the <command> tag (what program is running), ")
	b.WriteString("the <working_directory> tag (where it is running), ")
	b.WriteString("and the <terminal_output> tag (what is currently visible on screen). ")
	b.WriteString("Combine them into a task-oriented summary of the activity in progress — ")
	b.WriteString("for example \"reviewing git log in the roost repo\" or \"running make build, tests in progress\" — ")
	b.WriteString("not a verbatim quote of the screen. ")
	b.WriteString("Return only the body text, no headings, decoration, preamble, or quotes.\n\n")
	if prev != "" {
		b.WriteString("<previous_summary>\n")
		b.WriteString(prev)
		b.WriteString("\n</previous_summary>\n\n")
	}
	if command != "" {
		b.WriteString("<command>\n")
		b.WriteString(command)
		b.WriteString("\n</command>\n\n")
	}
	if workingDir != "" {
		b.WriteString("<working_directory>\n")
		b.WriteString(workingDir)
		b.WriteString("\n</working_directory>\n\n")
	}
	clipped := tailClip(strings.TrimSpace(content), summaryTotalCap)
	b.WriteString("<terminal_output>\n")
	b.WriteString(clipped)
	b.WriteString("\n</terminal_output>\n")
	return b.String()
}

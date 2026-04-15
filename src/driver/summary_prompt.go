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

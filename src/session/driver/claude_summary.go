package driver

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/take/agent-roost/lib/claude/cli"
	"github.com/take/agent-roost/lib/claude/transcript"
)

// Claude session summarizer.
//
// On every UserPromptSubmit hook the driver fires a one-shot
// `claude -p --model=haiku` subprocess in the background, feeding it the
// previous summary plus the most recent user/assistant rounds, and stores
// the resulting one-line summary on d.summary so the Card subtitle reflects
// "what is this session about" rather than just the latest user prompt.
//
// All work happens in a goroutine, the in-flight guard ensures at most one
// summarizer is active per session, and a 30s context bounds the wait.

const (
	// claudeKeySummary is the persisted-state bag key for the rolling
	// session summary string. Round-tripped through tmux user options so
	// warm/cold restarts restore the prior summary immediately.
	claudeKeySummary = "summary"

	// summaryUserRoundLim is the number of trailing user-prompt boundaries
	// to feed haiku. 2 covers the previous full round (user → assistant
	// chain) plus the brand-new user prompt that triggered the refresh.
	summaryUserRoundLim = 2

	// summaryEntryTextCap clips each individual entry's text from the tail
	// so a single very long assistant block can't bloat the prompt.
	summaryEntryTextCap = 1500

	// summaryTotalCap bounds the assembled prompt length. Older entries are
	// dropped from the front of the recent_turns block first; the newest
	// user prompt is always preserved.
	summaryTotalCap = 12000

	// summaryTimeout bounds the haiku subprocess. Tighter than the default
	// shell timeout because the summary is best-effort cosmetic data.
	summaryTimeout = 30 * time.Second
)

// summarizeFn is the package-level seam used to swap the real
// `claude -p --model=haiku` invocation for a stub in tests. Same pattern as
// claudeDriver.detectBranch.
var summarizeFn = cli.SummarizeWithHaiku

// triggerSummaryAsync kicks off a background haiku summarization for the
// current session. Drops the call if another summarization is already in
// flight or if the transcript hasn't produced any usable rounds yet.
//
// Lock discipline: never holds d.mu across the d.tracker.RecentRounds call
// — same convention as refreshMeta. The in-flight check is re-confirmed
// after the (mu-free) tracker call so two near-simultaneous prompts still
// produce at most one summarizer goroutine.
func (d *claudeDriver) triggerSummaryAsync() {
	d.mu.Lock()
	if d.summarizing {
		d.mu.Unlock()
		return
	}
	csid := d.claudeSessionID
	prev := d.summary
	d.mu.Unlock()
	if csid == "" {
		return
	}

	turns := d.tracker.RecentRounds(csid, summaryUserRoundLim)
	if len(turns) == 0 {
		return
	}
	prompt := formatSummaryPrompt(prev, turns)

	d.mu.Lock()
	if d.summarizing {
		// A racing call won the slot while we were assembling the prompt.
		// Drop ours — the in-flight one is fresher than nothing.
		d.mu.Unlock()
		return
	}
	d.summarizing = true
	sessionID := d.sessionID
	d.mu.Unlock()

	go d.runSummary(sessionID, prompt)
}

// runSummary executes the bounded haiku call and folds the result back into
// driver state. Errors are logged at debug level and leave the previous
// summary intact — the next user prompt will try again.
func (d *claudeDriver) runSummary(sessionID, prompt string) {
	ctx, cancel := context.WithTimeout(context.Background(), summaryTimeout)
	defer cancel()
	result, err := summarizeFn(ctx, prompt)

	d.mu.Lock()
	defer d.mu.Unlock()
	d.summarizing = false
	if err != nil {
		slog.Debug("claude driver: summary failed",
			"session", sessionID, "err", err)
		return
	}
	if result == "" {
		return
	}
	d.summary = result
}

// formatSummaryPrompt builds the haiku-bound prompt body. The output shape:
//
//	<instruction>
//	<previous_summary>...</previous_summary>   (omitted if prev is empty)
//	<recent_turns>
//	[user]
//	...
//	[assistant]
//	...
//	</recent_turns>
//
// Consecutive same-role entries collapse under one role header. Each entry's
// text is tail-clipped to summaryEntryTextCap characters; the assembled
// recent_turns block is then trimmed from the front (oldest entries first)
// until it fits within summaryTotalCap.
func formatSummaryPrompt(prev string, turns []transcript.TurnText) string {
	var b strings.Builder
	b.WriteString("あなたはセッション要約器です。以下の会話履歴と前回要約から、")
	b.WriteString("この AI コーディングセッションで現在ユーザーが何をしようとしているかを")
	b.WriteString("日本語で1行・40文字以内で要約してください。")
	b.WriteString("返答は要約文のみ、装飾・前置き・引用符なし。\n\n")

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

// renderRecentTurns formats the turns slice into the body of the
// <recent_turns> block, collapsing consecutive same-role entries. Applies
// per-entry clipping then total-size enforcement.
func renderRecentTurns(turns []transcript.TurnText) string {
	clipped := make([]transcript.TurnText, len(turns))
	for i, t := range turns {
		clipped[i] = transcript.TurnText{Role: t.Role, Text: tailClip(t.Text, summaryEntryTextCap)}
	}

	// Collapse consecutive same-role entries into role-prefixed blocks.
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

	// Enforce total cap by dropping blocks from the front (oldest first),
	// always retaining at least the final block (newest user prompt).
	body := strings.Join(blocks, "\n")
	for len(body) > summaryTotalCap && len(blocks) > 1 {
		blocks = blocks[1:]
		body = strings.Join(blocks, "\n")
	}
	return body
}

// tailClip returns the last `max` characters of s, prefixed with "…" when
// truncation occurs. Operates on runes so multibyte characters aren't split.
func tailClip(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return "…" + string(r[len(r)-max:])
}

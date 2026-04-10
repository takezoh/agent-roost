// Composed job runners. Each function constructs a Runner closure
// that composes lib functions into a complete job. The Pool dispatches
// to these via the Executor injected at construction.
//
// lib packages export raw functions (git.DetectBranch,
// cli.SummarizeWithHaiku, transcript.Tracker). This file combines
// them into coherent jobs.
package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/take/agent-roost/lib/claude/cli"
	"github.com/take/agent-roost/lib/claude/transcript"
	"github.com/take/agent-roost/lib/git"
	"github.com/take/agent-roost/driver"
)

// CapturePane runs tmux capture-pane and returns content + hash.
// captureFunc is typically tmuxBackend.CapturePane.
func CapturePane(captureFunc func(string, int) (string, error)) func(any) (any, error) {
	return func(input any) (any, error) {
		in := input.(driver.CapturePaneInput)
		content, err := captureFunc(string(in.WindowID), in.NLines)
		if err != nil {
			return nil, err
		}
		h := sha256.Sum256([]byte(content))
		return driver.CapturePaneResult{
			Content: content,
			Hash:    hex.EncodeToString(h[:]),
		}, nil
	}
}

// GitBranch detects the current git branch for a directory.
func GitBranch() func(any) (any, error) {
	return func(input any) (any, error) {
		in := input.(driver.GitBranchInput)
		return driver.GitBranchResult{Branch: git.DetectBranch(in.WorkingDir)}, nil
	}
}


// HaikuSummary composes transcript.Tracker (for recent conversation
// rounds) + cli.SummarizeWithHaiku (haiku subprocess) into a rolling
// session summary. Shares a Tracker with the TranscriptParse runner
// via the trackerRef parameter.
//
// Call NewHaikuSummary to get a matched pair of (TranscriptParse,
// HaikuSummary) runners that share the same Tracker.
func HaikuSummary(tracker *transcript.Tracker, mu *sync.Mutex) func(any) (any, error) {
	return func(input any) (any, error) {
		in := input.(driver.HaikuSummaryInput)

		// Pull recent conversation rounds from the shared tracker.
		mu.Lock()
		turns := tracker.RecentRounds(in.ClaudeUUID, 2)
		mu.Unlock()

		turns = appendHookPromptTurn(turns, in.CurrentPrompt)
		if len(turns) == 0 && in.PrevSummary == "" {
			return driver.HaikuSummaryResult{}, nil
		}
		prompt := formatSummaryPrompt(in.PrevSummary, turns)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := cli.SummarizeWithHaiku(ctx, prompt)
		if err != nil {
			return nil, err
		}
		return driver.HaikuSummaryResult{Summary: strings.TrimSpace(result)}, nil
	}
}

// NewClaudeRunners returns a matched pair of TranscriptParse and
// HaikuSummary runners that share the same Tracker. Use this from
// main.go when constructing the pool executor.
func NewClaudeRunners() (transcriptParse, haikuSummary func(any) (any, error)) {
	tracker := transcript.NewTracker()
	var mu sync.Mutex
	return TranscriptParseWithTracker(tracker, &mu), HaikuSummary(tracker, &mu)
}

// TranscriptParseWithTracker is like TranscriptParse but uses a
// provided tracker + mutex (for sharing with HaikuSummary).
func TranscriptParseWithTracker(tracker *transcript.Tracker, mu *sync.Mutex) func(any) (any, error) {
	return func(input any) (any, error) {
		in := input.(driver.TranscriptParseInput)
		mu.Lock()
		defer mu.Unlock()
		if _, err := tracker.Update(in.ClaudeUUID, in.Path); err != nil {
			return nil, err
		}
		snap := tracker.Snapshot(in.ClaudeUUID)
		return driver.TranscriptParseResult{
			Title:       snap.Title,
			LastPrompt:  snap.LastPrompt,
			StatusLine:  tracker.StatusLine(in.ClaudeUUID),
			CurrentTool: snap.Insight.CurrentTool,
			Subagents:   snap.Insight.SubagentCounts,
		}, nil
	}
}

// === Prompt assembly helpers (ported from old claude_summary.go) ===

const (
	summaryEntryTextCap = 1500
	summaryTotalCap     = 12000
)

func appendHookPromptTurn(turns []transcript.TurnText, hookPrompt string) []transcript.TurnText {
	if hookPrompt == "" {
		return turns
	}
	if n := len(turns); n > 0 && turns[n-1].Role == "user" && turns[n-1].Text == hookPrompt {
		return turns
	}
	return append(turns, transcript.TurnText{Role: "user", Text: hookPrompt})
}

func formatSummaryPrompt(prev string, turns []transcript.TurnText) string {
	var b strings.Builder
	b.WriteString("あなたはセッション要約器です。以下の会話履歴と前回要約から、")
	b.WriteString("この AI コーディングセッションで現在ユーザーが何をしようとしているかを")
	b.WriteString("日本語で 2〜3 行の説明的なメッセージにまとめてください。")
	b.WriteString("各行は別の観点（目的 / 直近の進捗 / 次の行動）を簡潔に述べ、")
	b.WriteString("各行 30 文字以内を目安にする。")
	b.WriteString("返答は本文のみ、見出し・装飾・前置き・引用符なし。\n\n")
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

func renderRecentTurns(turns []transcript.TurnText) string {
	clipped := make([]transcript.TurnText, len(turns))
	for i, t := range turns {
		clipped[i] = transcript.TurnText{Role: t.Role, Text: tailClip(t.Text, summaryEntryTextCap)}
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

// RegisterDefaults populates the executor with all built-in runners.
// capturePaneFn is the only external dependency (tmux backend).
// Adding a new job type = one Register call here, no switch anywhere.
func RegisterDefaults(exec *Executor, capturePaneFn func(string, int) (string, error)) {
	transcriptParse, haikuSummary := NewClaudeRunners()
	exec.Register(driver.TranscriptParseInput{}, transcriptParse)
	exec.Register(driver.HaikuSummaryInput{}, haikuSummary)
	exec.Register(driver.GitBranchInput{}, GitBranch())
	exec.Register(driver.CapturePaneInput{}, CapturePane(capturePaneFn))
}

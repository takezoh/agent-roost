// Composed job runners. Each function returns a typed runner closure
// that the effect interpreter passes to Submit[In, Out]. No reflect
// dispatch — the type switch lives in interpret.go.
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

	"github.com/take/agent-roost/driver"
	"github.com/take/agent-roost/lib/claude/cli"
	"github.com/take/agent-roost/lib/claude/transcript"
	"github.com/take/agent-roost/lib/git"
)

// Runners holds all runner functions used by the effect interpreter.
type Runners struct {
	CapturePane    func(driver.CapturePaneInput) (driver.CapturePaneResult, error)
	GitBranch      func(driver.GitBranchInput) (driver.GitBranchResult, error)
	TranscriptParse func(driver.TranscriptParseInput) (driver.TranscriptParseResult, error)
	HaikuSummary   func(driver.HaikuSummaryInput) (driver.HaikuSummaryResult, error)
}

// NewRunners constructs all runners. capturePaneFn is the tmux backend
// dependency for capture-pane. The TranscriptParse and HaikuSummary
// runners share a Tracker.
func NewRunners(capturePaneFn func(string, int) (string, error)) Runners {
	tp, hs := newClaudeRunners()
	return Runners{
		CapturePane:     newCapturePane(capturePaneFn),
		GitBranch:       newGitBranch(),
		TranscriptParse: tp,
		HaikuSummary:    hs,
	}
}

func newCapturePane(captureFunc func(string, int) (string, error)) func(driver.CapturePaneInput) (driver.CapturePaneResult, error) {
	return func(in driver.CapturePaneInput) (driver.CapturePaneResult, error) {
		content, err := captureFunc(string(in.WindowID), in.NLines)
		if err != nil {
			return driver.CapturePaneResult{}, err
		}
		h := sha256.Sum256([]byte(content))
		return driver.CapturePaneResult{
			Content: content,
			Hash:    hex.EncodeToString(h[:]),
		}, nil
	}
}

func newGitBranch() func(driver.GitBranchInput) (driver.GitBranchResult, error) {
	return func(in driver.GitBranchInput) (driver.GitBranchResult, error) {
		return driver.GitBranchResult{Branch: git.DetectBranch(in.WorkingDir)}, nil
	}
}

func newClaudeRunners() (
	func(driver.TranscriptParseInput) (driver.TranscriptParseResult, error),
	func(driver.HaikuSummaryInput) (driver.HaikuSummaryResult, error),
) {
	tracker := transcript.NewTracker()
	var mu sync.Mutex

	tp := func(in driver.TranscriptParseInput) (driver.TranscriptParseResult, error) {
		mu.Lock()
		defer mu.Unlock()
		if _, err := tracker.Update(in.ClaudeUUID, in.Path); err != nil {
			return driver.TranscriptParseResult{}, err
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

	hs := func(in driver.HaikuSummaryInput) (driver.HaikuSummaryResult, error) {
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
			return driver.HaikuSummaryResult{}, err
		}
		return driver.HaikuSummaryResult{Summary: strings.TrimSpace(result)}, nil
	}

	return tp, hs
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

package driver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/takezoh/agent-roost/lib/claude/cli"
	"github.com/takezoh/agent-roost/lib/claude/transcript"
	"github.com/takezoh/agent-roost/lib/vcs"
	"github.com/takezoh/agent-roost/runtime/worker"
	"github.com/takezoh/agent-roost/state"
)

var _ state.DriverState = GenericState{}

func RegisterRunners(capturePaneFn func(string, int) (string, error), language, summarizeCmd string) {
	worker.RegisterRunner("capture_pane", newCapturePane(capturePaneFn))
	tp, hs := newClaudeRunners(language, summarizeCmd)
	worker.RegisterRunner("transcript_parse", tp)
	worker.RegisterRunner("haiku_summary", hs)
	worker.RegisterRunner("branch_detect", newBranchDetect())
}

func newCapturePane(captureFunc func(string, int) (string, error)) func(CapturePaneInput) (CapturePaneResult, error) {
	return func(in CapturePaneInput) (CapturePaneResult, error) {
		content, err := captureFunc(in.WindowTarget, in.NLines)
		if err != nil {
			return CapturePaneResult{}, err
		}
		h := sha256.Sum256([]byte(content))
		return CapturePaneResult{
			Content: content,
			Hash:    hex.EncodeToString(h[:]),
		}, nil
	}
}

func newBranchDetect() func(BranchDetectInput) (BranchDetectResult, error) {
	return func(in BranchDetectInput) (BranchDetectResult, error) {
		r := vcs.DetectBranch(in.WorkingDir)
		return BranchDetectResult{
			Branch: r.Branch, Background: r.Background, Foreground: r.Foreground,
			IsWorktree: r.IsWorktree, ParentBranch: r.ParentBranch,
		}, nil
	}
}

func newClaudeRunners(language, summarizeCmd string) (
	func(TranscriptParseInput) (TranscriptParseResult, error),
	func(HaikuSummaryInput) (HaikuSummaryResult, error),
) {
	tracker := transcript.NewTracker()
	var mu sync.Mutex

	tp := func(in TranscriptParseInput) (TranscriptParseResult, error) {
		mu.Lock()
		defer mu.Unlock()
		if _, err := tracker.Update(in.ClaudeUUID, in.Path); err != nil {
			return TranscriptParseResult{}, err
		}
		snap := tracker.Snapshot(in.ClaudeUUID)
		return TranscriptParseResult{
			Title:       snap.Title,
			LastPrompt:  snap.LastPrompt,
			StatusLine:  tracker.StatusLine(in.ClaudeUUID),
			CurrentTool: snap.Insight.CurrentTool,
			Subagents:   snap.Insight.SubagentCounts,
		}, nil
	}

	hs := func(in HaikuSummaryInput) (HaikuSummaryResult, error) {
		if summarizeCmd == "" {
			return HaikuSummaryResult{}, nil
		}

		mu.Lock()
		turns := tracker.RecentRounds(in.ClaudeUUID, 2)
		mu.Unlock()

		turns = appendHookPromptTurn(turns, in.CurrentPrompt)
		if len(turns) == 0 && in.PrevSummary == "" {
			return HaikuSummaryResult{}, nil
		}
		prompt := formatSummaryPrompt(language, in.PrevSummary, turns)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := cli.SummarizeWithCommand(ctx, prompt, summarizeCmd)
		if err != nil {
			return HaikuSummaryResult{}, err
		}
		return HaikuSummaryResult{Summary: strings.TrimSpace(result)}, nil
	}

	return tp, hs
}

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

func formatSummaryPrompt(language string, prev string, turns []transcript.TurnText) string {
	var b strings.Builder
	b.WriteString("You are a session summarizer. From the conversation history and previous summary below, ")
	b.WriteString("summarize what the user is currently trying to do in this AI coding session ")
	b.WriteString("into a 2-3 line descriptive message in ")
	b.WriteString(language)
	b.WriteString(". ")
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

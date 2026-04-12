package driver

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/takezoh/agent-roost/lib/claude/cli"
	"github.com/takezoh/agent-roost/lib/claude/transcript"
)

func newTranscriptSummaryRunners(summarizeCmd string) (
	func(TranscriptParseInput) (TranscriptParseResult, error),
	func(SummaryCommandInput) (SummaryCommandResult, error),
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
			RecentTurns: normalizeTurns(tracker.RecentRounds(in.ClaudeUUID, 2)),
		}, nil
	}

	hs := func(in SummaryCommandInput) (SummaryCommandResult, error) {
		if summarizeCmd == "" || strings.TrimSpace(in.Prompt) == "" {
			return SummaryCommandResult{}, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, err := cli.SummarizeWithCommand(ctx, in.Prompt, summarizeCmd)
		if err != nil {
			return SummaryCommandResult{}, err
		}
		return SummaryCommandResult{Summary: strings.TrimSpace(result)}, nil
	}

	return tp, hs
}

func normalizeTurns(turns []transcript.TurnText) []SummaryTurn {
	if len(turns) == 0 {
		return nil
	}
	out := make([]SummaryTurn, len(turns))
	for i, t := range turns {
		out[i] = SummaryTurn{Role: t.Role, Text: t.Text}
	}
	return out
}

package driver

import (
	"context"
	"os"

	codextranscript "github.com/takezoh/agent-roost/lib/codex/transcript"
)

func newCodexTranscriptParse() func(context.Context, CodexTranscriptParseInput) (CodexTranscriptParseResult, error) {
	return func(ctx context.Context, in CodexTranscriptParseInput) (CodexTranscriptParseResult, error) {
		if err := ctx.Err(); err != nil {
			return CodexTranscriptParseResult{}, err
		}
		data, err := os.ReadFile(in.Path)
		if err != nil {
			return CodexTranscriptParseResult{}, err
		}
		parser := codextranscript.NewParser()
		parser.ParseLines(data)
		snap := parser.Snapshot()
		return CodexTranscriptParseResult{
			Title:                snap.Title,
			LastPrompt:           snap.LastPrompt,
			LastAssistantMessage: snap.LastAssistantMessage,
			StatusLine:           snap.StatusLine,
			RecentTurns:          normalizeCodexTurns(snap.RecentTurns),
		}, nil
	}
}

func normalizeCodexTurns(turns []codextranscript.TurnText) []SummaryTurn {
	if len(turns) == 0 {
		return nil
	}
	out := make([]SummaryTurn, len(turns))
	for i, t := range turns {
		out[i] = SummaryTurn{Role: t.Role, Text: t.Text}
	}
	return out
}

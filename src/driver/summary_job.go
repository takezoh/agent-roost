package driver

import (
	"strings"

	"github.com/takezoh/agent-roost/state"
)

func enqueueSummaryJob(
	effs []state.Effect,
	inFlight bool,
	conversationID string,
	prevSummary string,
	prompt string,
) ([]state.Effect, bool) {
	if inFlight || strings.TrimSpace(prompt) == "" {
		return effs, inFlight
	}
	effs = append(effs, state.EffStartJob{
		Input: HaikuSummaryInput{
			ClaudeUUID:    conversationID,
			PrevSummary:   prevSummary,
			CurrentPrompt: prompt,
		},
	})
	return effs, true
}

func applySummaryJobResult(summary string, inFlight bool, e state.DEvJobResult) (string, bool, bool) {
	r, ok := e.Result.(HaikuSummaryResult)
	if !ok {
		return summary, inFlight, false
	}
	if e.Err != nil {
		return summary, false, true
	}
	if r.Summary != "" {
		summary = r.Summary
	}
	return summary, false, true
}

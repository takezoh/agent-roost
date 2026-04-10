package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/take/agent-roost/state/driver"
)

// Job implementations. Each takes the worker Deps + a typed Input
// (asserted from any) and returns a typed Result + error.

func runCapturePane(deps Deps, input any) (any, error) {
	in, ok := input.(driver.CapturePaneInput)
	if !ok {
		return nil, errors.New("worker: capture-pane: input not CapturePaneInput")
	}
	if deps.Tmux == nil {
		return nil, errors.New("worker: capture-pane: no tmux backend")
	}
	content, err := deps.Tmux.CapturePane(string(in.WindowID), in.NLines)
	if err != nil {
		return nil, err
	}
	return driver.CapturePaneResult{
		Content: content,
		Hash:    hashContent(content),
	}, nil
}

func runHaikuSummary(deps Deps, input any) (any, error) {
	in, ok := input.(driver.HaikuSummaryInput)
	if !ok {
		return nil, errors.New("worker: haiku: input not HaikuSummaryInput")
	}
	if deps.Haiku == nil {
		return driver.HaikuSummaryResult{}, errors.New("worker: haiku: no runner configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := deps.Haiku.Summarize(ctx, in.Prompt)
	if err != nil {
		return nil, err
	}
	return driver.HaikuSummaryResult{Summary: strings.TrimSpace(out)}, nil
}

func runTranscriptParse(deps Deps, input any) (any, error) {
	in, ok := input.(driver.TranscriptParseInput)
	if !ok {
		return nil, errors.New("worker: transcript: input not TranscriptParseInput")
	}
	if deps.TranscriptDB == nil {
		return nil, errors.New("worker: transcript: no store")
	}
	snap, statusLine, err := deps.TranscriptDB.Parse(in.ClaudeUUID, in.Path)
	if err != nil {
		return nil, err
	}
	return driver.TranscriptParseResult{
		Title:       snap.Title,
		LastPrompt:  snap.LastPrompt,
		StatusLine:  statusLine,
		CurrentTool: snap.Insight.CurrentTool,
		Subagents:   snap.Insight.SubagentCounts,
	}, nil
}

func runGitBranch(deps Deps, input any) (any, error) {
	in, ok := input.(driver.GitBranchInput)
	if !ok {
		return nil, errors.New("worker: git-branch: input not GitBranchInput")
	}
	var branch string
	if deps.Branch != nil {
		branch = deps.Branch.Detect(in.WorkingDir)
	}
	return driver.GitBranchResult{Branch: branch}, nil
}

func hashContent(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

package driver

import "github.com/take/agent-roost/state"

// Job input/output types passed through state.EffStartJob.Input and
// state.EvJobResult.Result. Defined here (driver pkg) because both
// drivers and worker pool implementations import this package.
//
// Each *Input is what the reducer hands to the worker pool; each
// *Result is what the worker hands back via EvJobResult.

// CapturePaneInput asks the worker pool to run `tmux capture-pane`
// against a specific window's primary pane and return the trailing
// NLines lines. Used by polling drivers (e.g. genericDriver) to
// detect activity.
type CapturePaneInput struct {
	WindowID state.WindowID
	NLines   int
}

// CapturePaneResult carries the captured content. Hash is the SHA256
// of Content (computed by the worker so the reducer doesn't have to
// hash inside Step).
type CapturePaneResult struct {
	Content string
	Hash    string
}

// HaikuSummaryInput is the prompt body the haiku worker sends to the
// `claude -p --model=haiku` subprocess. The driver assembles it from
// the previous summary plus the recent turns; the worker just runs
// the subprocess and returns the trimmed output.
type HaikuSummaryInput struct {
	Prompt string
}

// HaikuSummaryResult is the trimmed summary string the haiku worker
// returns. Empty result is treated by the driver as "keep previous".
type HaikuSummaryResult struct {
	Summary string
}

// TranscriptParseInput points the worker at a session's transcript
// JSONL file. The worker maintains its own per-session Tracker and
// returns only the new content (deltas).
type TranscriptParseInput struct {
	SessionID    state.SessionID
	ClaudeUUID   string
	Path         string
}

// TranscriptParseResult is the parsed transcript snapshot the worker
// returns to the claude driver via DEvJobResult.
type TranscriptParseResult struct {
	Title       string
	LastPrompt  string
	StatusLine  string
	CurrentTool string
	Subagents   map[string]int
}

// GitBranchInput asks the worker to detect the current git branch for
// the given working directory.
type GitBranchInput struct {
	WorkingDir string
}

// GitBranchResult carries the detected branch name (empty when not a
// git repo or detection failed).
type GitBranchResult struct {
	Branch string
}

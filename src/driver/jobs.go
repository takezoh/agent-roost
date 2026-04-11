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

// HaikuSummaryInput carries everything the haiku worker needs to
// assemble and run the summary prompt. The worker uses ClaudeUUID
// to pull recent conversation rounds from its shared transcript
// Tracker, combines them with PrevSummary + CurrentPrompt, and
// sends the result to `claude -p --model=haiku`.
type HaikuSummaryInput struct {
	ClaudeUUID    string
	PrevSummary   string
	CurrentPrompt string
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

// BranchDetectInput asks the worker to detect the current VCS branch
// for the given working directory.
type BranchDetectInput struct {
	WorkingDir string
}

// BranchDetectResult carries the detected branch name and brand colors.
// All fields are empty when no VCS is detected.
type BranchDetectResult struct {
	Branch     string
	Background string // brand color hex
	Foreground string // text color hex
}

func (CapturePaneInput) JobKind() string     { return "capture_pane" }
func (TranscriptParseInput) JobKind() string { return "transcript_parse" }
func (HaikuSummaryInput) JobKind() string    { return "haiku_summary" }
func (BranchDetectInput) JobKind() string    { return "branch_detect" }

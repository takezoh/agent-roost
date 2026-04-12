package driver

import "github.com/takezoh/agent-roost/state"

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
	WindowTarget string
	NLines       int
}

// CapturePaneResult carries the captured content. Hash is the SHA256
// of Content (computed by the worker so the reducer doesn't have to
// hash inside Step).
type CapturePaneResult struct {
	Content string
	Hash    string
}

// SummaryCommandInput is the fully assembled prompt text. Prompt
// construction is driver-owned; the worker only pipes this prompt
// into the configured summarize command and returns stdout.
type SummaryCommandInput struct {
	Prompt string
}

// SummaryCommandResult is the trimmed summary string the summary worker
// returns. Empty result is treated by the driver as "keep previous".
type SummaryCommandResult struct {
	Summary string
}

// SummaryTurn is a normalized conversation turn used by summary prompt
// builders. The driver converts source-specific turn formats into this
// shape before constructing the prompt.
type SummaryTurn struct {
	Role string
	Text string
}

// TranscriptParseInput points the worker at a session's transcript
// JSONL file. The worker maintains its own per-session Tracker and
// returns only the new content (deltas).
type TranscriptParseInput struct {
	SessionID  state.SessionID
	ClaudeUUID string
	Path       string
}

// TranscriptParseResult is the parsed transcript snapshot the worker
// returns to the transcript-capable driver via DEvJobResult.
type TranscriptParseResult struct {
	Title       string
	LastPrompt  string
	StatusLine  string
	CurrentTool string
	Subagents   map[string]int
	RecentTurns []SummaryTurn
}

// BranchDetectInput asks the worker to detect the current VCS branch
// for the given working directory.
type BranchDetectInput struct {
	WorkingDir string
}

// BranchDetectResult carries the detected branch name and brand colors.
// All fields are empty when no VCS is detected.
type BranchDetectResult struct {
	Branch       string
	Background   string // brand color hex
	Foreground   string // text color hex
	IsWorktree   bool   // true when dir is a linked worktree
	ParentBranch string // branch of the main working tree
}

// WorktreeSetupInput asks the worker to create a managed git worktree
// for a codex session before tmux spawn.
type WorktreeSetupInput struct {
	RepoDir        string
	CandidateNames []string
}

// WorktreeSetupResult carries the created worktree path and chosen
// worktree name.
type WorktreeSetupResult struct {
	WorkingDir string
	Name       string
}

func (CapturePaneInput) JobKind() string     { return "capture_pane" }
func (TranscriptParseInput) JobKind() string { return "transcript_parse" }
func (SummaryCommandInput) JobKind() string  { return "summary_command" }
func (BranchDetectInput) JobKind() string    { return "branch_detect" }
func (WorktreeSetupInput) JobKind() string   { return "worktree_setup" }

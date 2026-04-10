package driver

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/take/agent-roost/state"
)

// Claude driver: event-driven status producer for the Claude Code CLI.
//
// All dynamic state lives in ClaudeState (a value type embedded in
// state.Session). Step is a pure function — it never touches the
// filesystem, never spawns subprocesses, never holds a goroutine.
// Side effects (transcript parsing, haiku summarization, branch
// detection, event log appends, fsnotify watch registration) are
// emitted as state.Effect values for the runtime interpreter.
//
// The driver is registered as a value in init(); the same value serves
// every Claude session in the daemon process.
const (
	// PersistedState bag keys for sessions.json round-trip.
	claudeKeyClaudeSessionID = "claude_session_id"
	claudeKeyWorkingDir      = "working_dir"
	claudeKeyTranscriptPath  = "transcript_path"
	claudeKeyStatus          = "status"
	claudeKeyStatusChangedAt = "status_changed_at"
	claudeKeyBranchTag       = "branch_tag"
	claudeKeyBranchTarget    = "branch_target"
	claudeKeyBranchAt        = "branch_at"
	claudeKeySummary         = "summary"

	// Re-detect branch at most every N seconds (only when active).
	claudeBranchRefreshInterval = 30 * time.Second
)

// ClaudeState is the per-session private state for the Claude driver.
// Plain data — no goroutines, no I/O. Embeds state.DriverStateBase to
// satisfy the sealed state.DriverState interface.
type ClaudeState struct {
	state.DriverStateBase

	// Identity (set via Restore or DEvHook session-start payload).
	ClaudeSessionID string // distinct from roost session id; the *Claude* conversation id
	WorkingDir      string
	TranscriptPath  string

	// Status bookkeeping
	Status          state.Status
	StatusChangedAt time.Time

	// Cached transcript meta (folded in by DEvJobResult{JobTranscriptParse})
	Title          string
	LastPrompt     string
	StatusLine     string
	CurrentTool    string
	SubagentCounts map[string]int

	// Branch tag cache
	BranchTag    string
	BranchTarget string
	BranchAt     time.Time

	// Summary cache + in-flight guards. Each *InFlight bool prevents
	// duplicate jobs from being scheduled while one is still pending.
	Summary             string
	SummaryInFlight     bool
	TranscriptInFlight  bool
	BranchInFlight      bool
	WatchedTranscript   string // currently fsnotify-watched path; empty = not watched
}

// ClaudeDriver is the stateless plugin value. The home directory is
// captured at construction so resolveTranscriptPath can build the
// canonical ~/.claude/projects/... path when the agent hasn't reported
// transcript_path yet.
type ClaudeDriver struct {
	home string
}

// NewClaudeDriver constructs a Claude driver bound to the user's home
// directory. The runtime constructs one of these at startup and
// registers it with state.Register.
func NewClaudeDriver(home string) ClaudeDriver {
	return ClaudeDriver{home: home}
}

func (ClaudeDriver) Name() string        { return "claude" }
func (ClaudeDriver) DisplayName() string { return "claude" }

// View returns the cached View for the given ClaudeState. Pure
// getter — same payload Step would return, but callable from the
// runtime without going through Step.
func (d ClaudeDriver) View(s state.DriverState) state.View {
	cs, ok := s.(ClaudeState)
	if !ok {
		cs = ClaudeState{}
	}
	return d.view(cs)
}

func (d ClaudeDriver) NewState(now time.Time) state.DriverState {
	return ClaudeState{
		Status:          state.StatusIdle,
		StatusChangedAt: now,
	}
}

// SpawnCommand returns "claude --resume <id>" when an agent session ID
// is known so cold-boot recovery picks up the prior conversation.
// Mirrors lib/claude/cli.ResumeCommand exactly so we don't take a
// dependency on lib/claude/cli from the pure-state layer.
func (d ClaudeDriver) SpawnCommand(s state.DriverState, baseCommand string) string {
	cs, ok := s.(ClaudeState)
	if !ok || cs.ClaudeSessionID == "" {
		return baseCommand
	}
	// We assume baseCommand starts with "claude" — if it carries flags
	// already, drop --resume so users don't get duplicate flags.
	if strings.Contains(baseCommand, "--resume") {
		return baseCommand
	}
	return baseCommand + " --resume " + cs.ClaudeSessionID
}

// Step is the pure reducer for the Claude driver. The hook event
// dispatch lives in claude_event.go; transcript / summary / branch
// result handling lives in their respective files.
func (d ClaudeDriver) Step(prev state.DriverState, ev state.DriverEvent) (state.DriverState, []state.Effect, state.View) {
	cs, ok := prev.(ClaudeState)
	if !ok {
		cs = d.NewState(time.Time{}).(ClaudeState)
	}

	switch e := ev.(type) {
	case state.DEvHook:
		next, effs := d.handleHook(cs, e)
		return next, effs, d.view(next)

	case state.DEvTick:
		next, effs := d.handleTick(cs, e)
		return next, effs, d.view(next)

	case state.DEvTranscriptChanged:
		next, effs := d.handleTranscriptChanged(cs, e)
		return next, effs, d.view(next)

	case state.DEvJobResult:
		next, effs := d.handleJobResult(cs, e)
		return next, effs, d.view(next)
	}

	return cs, nil, d.view(cs)
}

// resolveTranscriptPath picks the best known transcript path. Priority:
//  1. Agent-reported path (canonical, handles --worktree)
//  2. Computed from working_dir + claudeSessionID
//  3. "" if neither is available
func (d ClaudeDriver) resolveTranscriptPath(cs ClaudeState) string {
	if cs.TranscriptPath != "" {
		return cs.TranscriptPath
	}
	if d.home == "" || cs.ClaudeSessionID == "" || cs.WorkingDir == "" {
		return ""
	}
	return filepath.Join(d.home, ".claude", "projects", projectDir(cs.WorkingDir), cs.ClaudeSessionID+".jsonl")
}

// projectDir mirrors Claude Code's encoding of working dir →
// ~/.claude/projects/ dir name: replace / and . with -.
func projectDir(p string) string {
	return strings.NewReplacer("/", "-", ".", "-").Replace(p)
}

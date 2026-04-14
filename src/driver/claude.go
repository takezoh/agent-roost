package driver

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/takezoh/agent-roost/state"
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
	ClaudeDriverName = "claude"

	// PersistedState bag keys for sessions.json round-trip.
	claudeKeyClaudeSessionID = "claude_session_id"

	// Re-detect branch at most every N seconds (only when active).
	claudeBranchRefreshInterval = 30 * time.Second

	// Hang detection: if the pane content hasn't changed for this long
	// while the session is Running (and no subagents are active), treat
	// the agent as stale and transition to Idle.
	claudeHangThreshold = 120 * time.Second
)

// ClaudeState is the per-session private state for the Claude driver.
// Plain data — no goroutines, no I/O. Embeds CommonState to
// satisfy the sealed state.DriverState interface.
type ClaudeState struct {
	CommonState

	// Identity (set via Restore or DEvHook session-start payload).
	ClaudeSessionID string // distinct from roost session id; the *Claude* conversation id

	// Hook ordering: stale events (Timestamp <= LastBridgeTS) are dropped.
	LastBridgeTS time.Time

	// Cached transcript meta (folded in by DEvJobResult{JobTranscriptParse})
	StatusLine     string
	CurrentTool    string
	SubagentCounts map[string]int
	RecentTurns    []SummaryTurn

	// Summary cache + in-flight guards. Each *InFlight bool prevents
	// duplicate jobs from being scheduled while one is still pending.
	TranscriptInFlight bool
	CaptureInFlight    bool
	WatchedFile        string // currently fsnotify-watched path; empty = not watched

	// Hang detection: pane-capture hash comparison for background sessions.
	PaneHash     string    // SHA256 of last captured pane content
	PaneHashAt   time.Time // when PaneHash last changed (or first set)
	HangDetected bool      // set when hang threshold fires; cleared on next hook
}

// ClaudeDriver is the stateless plugin value. The home directory is
// captured at construction so resolveTranscriptPath can build the
// canonical ~/.claude/projects/... path when the agent hasn't reported
// transcript_path yet.
// ClaudeOptions holds driver-specific config decoded from [drivers.claude].
type ClaudeOptions struct {
	ShowThinking     bool   `json:"show_thinking"`
	SummarizeCommand string `json:"summarize_command"`
}

type ClaudeDriver struct {
	home         string
	eventLogDir  string
	showThinking bool
}

// NewClaudeDriver constructs a Claude driver bound to the user's home
// directory and event log directory. The runtime constructs one of
// these at startup and registers it with state.Register.
func NewClaudeDriver(home, eventLogDir string, opts ClaudeOptions) ClaudeDriver {
	return ClaudeDriver{home: home, eventLogDir: eventLogDir, showThinking: opts.ShowThinking}
}

func (ClaudeDriver) Name() string                            { return ClaudeDriverName }
func (ClaudeDriver) DisplayName() string                     { return ClaudeDriverName }
func (ClaudeDriver) Status(s state.DriverState) state.Status { return s.(ClaudeState).Status }

func (ClaudeDriver) StartDir(s state.DriverState) string {
	cs, ok := s.(ClaudeState)
	if !ok {
		return ""
	}
	return cs.CommonState.StartDir
}

func (ClaudeDriver) WithStartDir(s state.DriverState, dir string) state.DriverState {
	cs, ok := s.(ClaudeState)
	if !ok {
		return s
	}
	cs.CommonState.StartDir = dir
	return cs
}

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
		CommonState: CommonState{
			Status:          state.StatusIdle,
			StatusChangedAt: now,
		},
	}
}

// SpawnCommand returns "claude --resume <id>" when an agent session ID
// is known so cold-boot recovery picks up the prior conversation.
// Mirrors lib/claude/cli.ResumeCommand exactly so we don't take a
// dependency on lib/claude/cli from the pure-state layer.
func (d ClaudeDriver) PrepareLaunch(s state.DriverState, mode state.LaunchMode, project, baseCommand string, options state.LaunchOptions) (state.LaunchPlan, error) {
	cs, ok := s.(ClaudeState)
	if !ok {
		cs = ClaudeState{}
	}
	startDir := project
	if cs.StartDir != "" {
		startDir = cs.StartDir
	}
	req, stripped := resolveWorktreeRequest(baseCommand, options, "--worktree")
	command := appendFlag(stripped, "--worktree", req.Enabled)
	if mode != state.LaunchModeColdStart || cs.ClaudeSessionID == "" {
		return state.LaunchPlan{Command: command, StartDir: startDir, Stdin: options.InitialInput}, nil
	}
	command = strings.TrimSpace(stripWorktreeFlag(command))
	if strings.Contains(command, "--resume") || !isAlphanumHyphen(cs.ClaudeSessionID) {
		return state.LaunchPlan{Command: command, StartDir: startDir, Stdin: options.InitialInput}, nil
	}
	path := d.resolveTranscriptPath(cs)
	if path == "" {
		return state.LaunchPlan{Command: command, StartDir: startDir, Stdin: options.InitialInput}, nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return state.LaunchPlan{Command: command, StartDir: startDir, Stdin: options.InitialInput}, nil
		}
		return state.LaunchPlan{}, err
	}
	return state.LaunchPlan{
		Command:  command + " --resume " + cs.ClaudeSessionID,
		StartDir: startDir,
		Options:  state.LaunchOptions{},
		Stdin:    options.InitialInput,
	}, nil
}

// stripWorktreeFlag removes --worktree (and its optional name
// argument) from a command string. Mirrors the logic in
// lib/claude/cli.StripWorktreeFlag but duplicated here so
// state/driver stays a leaf package.
func stripWorktreeFlag(command string) string {
	_, stripped := parseWorktreeFlags(command, "--worktree")
	return stripped
}

func isAlphanumHyphen(s string) bool {
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return len(s) > 0
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

	case state.DEvFileChanged:
		next, effs := d.handleTranscriptChanged(cs, e)
		return next, effs, d.view(next)

	case state.DEvJobResult:
		next, effs := d.handleJobResult(cs, e)
		return next, effs, d.view(next)
	}

	return cs, nil, d.view(cs)
}

func (d ClaudeDriver) WarmStartRecover(s state.DriverState, now time.Time) (state.DriverState, []state.Effect) {
	cs, ok := s.(ClaudeState)
	if !ok {
		cs = d.NewState(now).(ClaudeState)
	}
	path := d.resolveTranscriptPath(cs)
	if path == "" {
		return cs, nil
	}
	if cs.TranscriptPath == "" {
		cs.TranscriptPath = path
	}
	var effs []state.Effect
	if cs.WatchedFile != path {
		cs.WatchedFile = path
		effs = append(effs, state.EffWatchFile{Path: path, Kind: "transcript"})
	}
	if !cs.TranscriptInFlight {
		cs.TranscriptInFlight = true
		effs = append(effs, state.EffStartJob{
			Input: TranscriptParseInput{
				ClaudeUUID: cs.ClaudeSessionID,
				Path:       path,
			},
		})
	}
	return cs, effs
}

// resolveTranscriptPath picks the best known transcript path. Priority:
//  1. Agent-reported path (canonical, handles --worktree)
//  2. Computed from working_dir + claudeSessionID
//  3. "" if neither is available
func (d ClaudeDriver) resolveTranscriptPath(cs ClaudeState) string {
	if cs.TranscriptPath != "" {
		return cs.TranscriptPath
	}
	if d.home == "" || cs.ClaudeSessionID == "" || cs.StartDir == "" {
		return ""
	}
	return filepath.Join(d.home, ".claude", "projects", projectDir(cs.StartDir), cs.ClaudeSessionID+".jsonl")
}

// projectDir mirrors Claude Code's encoding of working dir →
// ~/.claude/projects/ dir name: replace / and . with -.
func projectDir(p string) string {
	return strings.NewReplacer("/", "-", ".", "-").Replace(p)
}

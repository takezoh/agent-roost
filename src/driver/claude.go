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
)

// pendingTool tracks an in-flight tool call from PreToolUse until
// its corresponding PostToolUse or PostToolUseFailure arrives.
// Keyed by tool_use_id in ClaudeState.PendingTools.
// Not persisted — orphaned entries are handled gracefully.
type pendingTool struct {
	Name      string
	Input     map[string]any
	StartedAt time.Time
	SawPrompt bool   // true if a permission_prompt Notification was observed
	PermMode  string // permission_mode from the PreToolUse payload
}

// ClaudeState is the per-session private state for the Claude driver.
// Plain data — no goroutines, no I/O. Embeds CommonState to
// satisfy the sealed state.DriverState interface.
type ClaudeState struct {
	CommonState

	ManagedWorkingDir string // set when roost pre-created the git worktree

	// Identity (set via Restore or DEvHook session-start payload).
	ClaudeSessionID string // distinct from roost session id; the *Claude* conversation id

	// Hook ordering: stale events (Timestamp <= LastBridgeTS) are dropped.
	LastBridgeTS time.Time

	// Cached transcript meta (folded in by DEvJobResult{JobTranscriptParse})
	StatusLine     string
	CurrentTool    string
	SubagentCounts map[string]int
	RecentTurns    []SummaryTurn
	// PlanFile is the latest ~/.claude/plans/*.md path seen in Write tool calls.
	PlanFile string

	// Summary cache + in-flight guards. Each *InFlight bool prevents
	// duplicate jobs from being scheduled while one is still pending.
	TranscriptInFlight bool
	WatchedFile        string // currently fsnotify-watched path; empty = not watched

	// PendingTools tracks tool calls between PreToolUse and their
	// matching Post* event, keyed by tool_use_id. Ephemeral — cleared
	// on SessionStart/SessionEnd; entries that never receive a Post are
	// abandoned silently (e.g. daemon restart mid-tool).
	PendingTools map[string]pendingTool

	// LastWindowTitle is the most-recently seen OSC 0 title from the pane.
	// Used to deduplicate rapid spinner updates so only meaningful transitions
	// (Braille → ✳ or vice-versa) trigger a status change.
	LastWindowTitle string
}

// ClaudeDriver is the stateless plugin value. The home directory is
// captured at construction so resolveTranscriptPath can build the
// canonical ~/.claude/projects/... path when the agent hasn't reported
// transcript_path yet.
// ClaudeOptions holds driver-specific config decoded from [drivers.claude].
type ClaudeOptions struct {
	ShowThinking bool `json:"show_thinking"`
}

type ClaudeDriver struct {
	home         string
	eventLogDir  string
	showThinking bool
	pager        string // command to open plan files (e.g. "less")
}

// NewClaudeDriver constructs a Claude driver bound to the user's home
// directory and event log directory. The runtime constructs one of
// these at startup and registers it with state.Register.
func NewClaudeDriver(home, eventLogDir string, opts ClaudeOptions, pager string) ClaudeDriver {
	return ClaudeDriver{home: home, eventLogDir: eventLogDir, showThinking: opts.ShowThinking, pager: pager}
}

func (ClaudeDriver) Name() string                            { return ClaudeDriverName }
func (ClaudeDriver) DisplayName() string                     { return ClaudeDriverName }
func (ClaudeDriver) Status(s state.DriverState) state.Status { return s.(ClaudeState).Status }

func (ClaudeDriver) StartDir(s state.DriverState) string {
	cs, ok := s.(ClaudeState)
	if !ok {
		return ""
	}
	return cs.StartDir
}

func (ClaudeDriver) WithStartDir(s state.DriverState, dir string) state.DriverState {
	cs, ok := s.(ClaudeState)
	if !ok {
		return s
	}
	cs.StartDir = dir
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
	command := stripped
	if cs.ManagedWorkingDir == "" {
		command = appendFlag(stripped, "--worktree", req.Enabled)
	}
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
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			return false
		}
	}
	return len(s) > 0
}

// Step is the pure reducer for the Claude driver. The hook event
// dispatch lives in claude_event.go; transcript / summary / branch
// result handling lives in their respective files.
func (d ClaudeDriver) Step(prev state.DriverState, ctx state.FrameContext, ev state.DriverEvent) (state.DriverState, []state.Effect, state.View) {
	cs, ok := prev.(ClaudeState)
	if !ok {
		cs = d.NewState(time.Time{}).(ClaudeState)
	}

	switch e := ev.(type) {
	case state.DEvHook:
		next, effs := d.handleHook(cs, ctx, e)
		return next, effs, d.view(next)

	case state.DEvTick:
		if !ctx.IsRoot {
			return cs, nil, d.view(cs)
		}
		next, effs := d.handleTick(cs, e)
		return next, effs, d.view(next)

	case state.DEvPaneActivity:
		if !ctx.IsRoot {
			return cs, nil, d.view(cs)
		}
		effs := cs.HandleActivity(e)
		return cs, effs, d.view(cs)

	case state.DEvFileChanged:
		next, effs := d.handleTranscriptChanged(cs, e)
		return next, effs, d.view(next)

	case state.DEvJobResult:
		next, effs := d.handleJobResult(cs, e)
		return next, effs, d.view(next)

	case state.DEvPaneOsc:
		if !ctx.IsRoot {
			return cs, nil, d.view(cs)
		}
		next := d.handleWindowTitle(cs, e.Title, e.Now)
		return next, nil, d.view(next)

	case state.DEvStatusLineClick:
		next, effs := d.handleStatusLineClick(cs, e)
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
//  1. Agent-reported path, if it exists on the local filesystem.
//  2. Computed from d.home (daemon's host HOME) + claudeSessionID.
//  3. "" if neither is available.
//
// Priority 1 may fall through to 2 when claude runs inside a Docker sandbox:
// the agent reports a path under the container HOME (e.g. /home/user/.claude/…)
// which does not exist on the host, while the actual file lives under the host
// HOME via a bind mount (e.g. /home/take/.claude/…).
func (d ClaudeDriver) resolveTranscriptPath(cs ClaudeState) string {
	if cs.TranscriptPath != "" {
		if _, err := os.Stat(cs.TranscriptPath); err == nil {
			return cs.TranscriptPath
		}
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

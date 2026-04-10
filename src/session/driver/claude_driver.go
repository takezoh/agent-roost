package driver

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/take/agent-roost/lib/claude/cli"
	"github.com/take/agent-roost/lib/claude/hookevent"
	"github.com/take/agent-roost/lib/claude/transcript"
	"github.com/take/agent-roost/lib/git"
)

// Claude driver: event-driven status producer for the Claude Code CLI.
//
// All dynamic state lives in the instance and is mutated by HandleEvent
// (hook events from `roost claude event`) and refreshed (transcript meta)
// from Tick. Construction never touches I/O — RestorePersistedState is the
// only path that fills the instance from external storage.
//
// claudeDriver is intentionally split across multiple files:
//   - claude_driver.go    — Driver interface implementation (lifecycle / Status / persistence)
//   - claude_view.go      — View() construction (Card / InfoExtras / LogTabs / StatusLine)
//   - claude_branch.go    — branch detection + cache
//   - claude_eventlog.go  — event log writer (lazy file open + append)

const (
	claudeNamePromptPattern = `(?m)(^>|❯\s*$)`

	// PersistedState keys. Defined here so adding a Claude-specific
	// persisted field is a single-file change. SessionService treats
	// the bag as opaque.
	claudeKeySessionID       = "session_id"
	claudeKeyWorkingDir      = "working_dir"
	claudeKeyTranscriptPath  = "transcript_path"
	claudeKeyStatus          = "status"
	claudeKeyStatusChangedAt = "status_changed_at"
	claudeKeyBranchTag       = "branch_tag"
	claudeKeyBranchTarget    = "branch_target"
	claudeKeyBranchAt        = "branch_at"
	// claudeKeyHookEventJSON carries the raw hook payload bytes from the
	// hook bridge so the driver can inspect fields beyond the pre-derived
	// Status (e.g. hook_event_name). Volatile — not persisted.
	claudeKeyHookEventJSON = "hook_event_json"

	// Refresh transcript metadata every N ticks (~5 seconds at 1 Hz).
	claudeMetaRefreshTicks = 5

	// Re-detect branch at most every N (only when active).
	claudeBranchRefreshInterval = 30 * time.Second
)

type claudeDriver struct {
	mu sync.Mutex

	// Static deps
	home        string // for ~/.claude/projects/... resolution
	tracker     *transcript.Tracker
	sessionCtx  SessionContext // pull-based active-state query (never nil)
	sessionID   string         // cached from sessionCtx.ID() at construction
	eventLogDir string         // base dir; the per-session file path is derived from sessionID

	// detectBranch defaults to git.DetectBranch but is a field so tests
	// (in-package) can stub it without forking real git. Production
	// callers never override the default.
	detectBranch func(dir string) string

	// Identity (set via RestorePersistedState or HandleEvent)
	claudeSessionID string // distinct from sessionID: the *Claude* conversation id used by --resume
	workingDir      string
	transcriptPath  string

	// Dynamic state
	status         StatusInfo
	title          string
	lastPrompt     string
	statusLine     string
	currentTool    string
	subagentCounts map[string]int
	tickCounter    int

	// lastHookEvent is the most recent hook payload received from the
	// bridge, parsed from claudeKeyHookEventJSON. Read-only after
	// absorbDriverStateLocked. Volatile: cleared on parse failure, never
	// persisted across restarts.
	lastHookEvent hookevent.HookEvent

	// Branch tag cache (see claude_branch.go)
	branchTag    string
	branchTarget string
	branchAt     time.Time

	// Summary state (see claude_summary.go). summary is the last haiku-
	// generated session summary string shown as Card.Subtitle. summarizing
	// is the in-flight guard so concurrent UserPromptSubmit hooks don't
	// each spawn a duplicate summarizer.
	summary     string
	summarizing bool

	// Event log writer state (see claude_eventlog.go)
	eventLogMu sync.Mutex
	eventLogF  *os.File
}

func newClaudeFactory() Factory {
	return func(deps Deps) Driver {
		now := time.Now()
		ctx := deps.Session
		if ctx == nil {
			ctx = inactiveSessionContext{}
		}
		return &claudeDriver{
			home:         deps.Home,
			tracker:      transcript.NewTracker(),
			sessionCtx:   ctx,
			sessionID:    ctx.ID(),
			eventLogDir:  deps.EventLogDir,
			detectBranch: git.DetectBranch,
			status:       StatusInfo{Status: StatusIdle, ChangedAt: now},
		}
	}
}

func (d *claudeDriver) Name() string        { return "claude" }
func (d *claudeDriver) DisplayName() string { return "claude" }

// MarkSpawned: a fresh agent process has just started. Reset to Idle (the
// next hook event will report the actual state). Identity is preserved if a
// prior session was restored — Claude resumes via --resume <id>.
func (d *claudeDriver) MarkSpawned() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.status = StatusInfo{Status: StatusIdle, ChangedAt: time.Now()}
}

// Tick: Claude is event-driven, so the only periodic work is the
// transcript refresh that picks up title / lastPrompt / insight changes
// from JSONL deltas + branch detection. Both are gated on the session
// being currently active (swapped into pane 0.0) so background sessions
// don't pay for parsing or git they will never display.
//
// Coordinator continues to fan Tick out to every Driver every second; the
// early return below means inactive Drivers cost only one interface
// dispatch + one Active() lookup per tick (negligible).
func (d *claudeDriver) Tick(now time.Time, win WindowInfo) {
	if !d.sessionCtx.Active() {
		return
	}
	d.refreshBranch(now, projectFromWindow(win))
	d.mu.Lock()
	d.tickCounter++
	if d.tickCounter%claudeMetaRefreshTicks != 0 {
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()
	d.refreshMeta()
}

// projectFromWindow returns win.Project() if win is non-nil, otherwise "".
// Tests may pass nil to exercise the no-window path.
func projectFromWindow(win WindowInfo) string {
	if win == nil {
		return ""
	}
	return win.Project()
}

// HandleEvent: classify the hook payload and update private state.
//   - SessionStart: pick up identity keys (session_id / working_dir /
//     transcript_path) from DriverState
//   - StateChange: parse the State field into a Status, update status +
//     changed_at, and trigger a transcript refresh so the user sees the new
//     title/insight without waiting for the next periodic tick
func (d *claudeDriver) HandleEvent(ev AgentEvent) bool {
	switch ev.Type {
	case AgentEventSessionStart:
		d.mu.Lock()
		d.absorbDriverStateLocked(ev.DriverState)
		d.mu.Unlock()
		slog.Debug("claude driver: session start",
			"session", d.sessionID, "claude_session", d.claudeSessionID)
		// Trigger an immediate meta refresh: SessionStart often arrives
		// before any state-change, and we want the title chip populated
		// as soon as the transcript file appears.
		d.refreshMeta()
		d.appendEventLog("SessionStart")
		return true
	case AgentEventStateChange:
		status, ok := ParseStatus(ev.State)
		if !ok {
			slog.Warn("claude driver: unparseable state",
				"session", d.sessionID, "state", ev.State, "log", ev.Log)
			return false
		}
		d.mu.Lock()
		prev := d.status.Status
		d.absorbDriverStateLocked(ev.DriverState)
		d.status = StatusInfo{Status: status, ChangedAt: time.Now()}
		// UserPromptSubmit is the only state-change that should trigger a
		// new haiku summary refresh. PreToolUse / PostToolUse / Stop also
		// arrive as state changes but we don't want to re-summarize on each
		// tool tick — only when the user themselves has just spoken. Capture
		// the hook prompt and the isUserPrompt flag while still holding the
		// lock so a racing HandleEvent (separate goroutine per IPC client)
		// can't overwrite lastHookEvent before we read it. Also seed
		// d.lastPrompt from the hook now so subsequent View() calls see it
		// without waiting for refreshMeta to fold the new turn into the
		// JSONL — Claude often writes the prompt to the file *after* firing
		// the hook.
		isUserPrompt := d.lastHookEvent.HookEventName == "UserPromptSubmit"
		hookPrompt := d.lastHookEvent.Prompt
		if isUserPrompt && hookPrompt != "" {
			d.lastPrompt = hookPrompt
		}
		d.mu.Unlock()
		slog.Debug("claude driver: state change",
			"session", d.sessionID, "from", prev.String(),
			"to", status.String(), "log", ev.Log)
		d.refreshMeta()
		if isUserPrompt {
			d.triggerSummaryAsync(hookPrompt)
		}
		logLine := ev.Log
		if logLine == "" {
			logLine = ev.State
		}
		d.appendEventLog(logLine)
		return true
	}
	slog.Debug("claude driver: unknown event type", "session", d.sessionID, "type", ev.Type)
	return false
}

func (d *claudeDriver) absorbDriverStateLocked(ds map[string]string) {
	if ds == nil {
		return
	}
	if v, ok := ds[claudeKeySessionID]; ok && v != "" {
		d.claudeSessionID = v
	}
	if v, ok := ds[claudeKeyWorkingDir]; ok && v != "" {
		d.workingDir = v
	}
	if v, ok := ds[claudeKeyTranscriptPath]; ok && v != "" {
		d.transcriptPath = v
	}
	if v, ok := ds[claudeKeyHookEventJSON]; ok && v != "" {
		ev, err := hookevent.ParseHookEvent([]byte(v))
		if err != nil {
			// Fail open: the bridge already pre-derived Status, so a
			// malformed payload only loses the auxiliary inspection
			// surface. Drop the previous parse so callers don't read
			// stale data.
			slog.Debug("claude driver: hook_event_json parse failed",
				"session", d.sessionID, "err", err)
			d.lastHookEvent = hookevent.HookEvent{}
		} else {
			d.lastHookEvent = ev
		}
	}
}

func (d *claudeDriver) Close() {
	d.mu.Lock()
	csid := d.claudeSessionID
	d.mu.Unlock()
	if d.tracker != nil && csid != "" {
		d.tracker.Forget(csid)
	}
	d.closeEventLog()
}

func (d *claudeDriver) Status() (StatusInfo, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.status, true
}

// PersistedState returns the opaque bag SessionService rounds-trips through
// tmux user options + sessions.json. Includes status so warm/cold restart
// restores the prior status without resetting to Idle, plus the cached
// branch tag so the user sees the prior branch immediately on restart.
func (d *claudeDriver) PersistedState() map[string]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[string]string, 8)
	if d.claudeSessionID != "" {
		out[claudeKeySessionID] = d.claudeSessionID
	}
	if d.workingDir != "" {
		out[claudeKeyWorkingDir] = d.workingDir
	}
	if d.transcriptPath != "" {
		out[claudeKeyTranscriptPath] = d.transcriptPath
	}
	out[claudeKeyStatus] = d.status.Status.String()
	if !d.status.ChangedAt.IsZero() {
		out[claudeKeyStatusChangedAt] = d.status.ChangedAt.UTC().Format(time.RFC3339)
	}
	if d.branchTag != "" {
		out[claudeKeyBranchTag] = d.branchTag
	}
	if d.branchTarget != "" {
		out[claudeKeyBranchTarget] = d.branchTarget
	}
	if !d.branchAt.IsZero() {
		out[claudeKeyBranchAt] = d.branchAt.UTC().Format(time.RFC3339)
	}
	if d.summary != "" {
		out[claudeKeySummary] = d.summary
	}
	return out
}

// RestorePersistedState rehydrates identity + status + branch cache from
// the opaque bag previously returned by PersistedState. Empty maps leave
// the factory defaults intact.
func (d *claudeDriver) RestorePersistedState(state map[string]string) {
	if len(state) == 0 {
		return
	}
	d.mu.Lock()
	d.absorbDriverStateLocked(state)
	if s, ok := state[claudeKeyStatus]; ok && s != "" {
		if status, ok := ParseStatus(s); ok {
			changedAt, _ := time.Parse(time.RFC3339, state[claudeKeyStatusChangedAt])
			if changedAt.IsZero() {
				changedAt = time.Now()
			}
			d.status = StatusInfo{Status: status, ChangedAt: changedAt}
		}
	}
	d.branchTag = state[claudeKeyBranchTag]
	d.branchTarget = state[claudeKeyBranchTarget]
	if at, err := time.Parse(time.RFC3339, state[claudeKeyBranchAt]); err == nil {
		d.branchAt = at
	}
	if s, ok := state[claudeKeySummary]; ok {
		d.summary = s
	}
	d.mu.Unlock()
	// Pre-populate transcript meta so the UI shows the prior title/insight
	// immediately on restart, without waiting for the first periodic tick.
	d.refreshMeta()
}

// SpawnCommand returns "claude --resume <id>" when an agent session ID is
// known so cold-boot recovery picks up the prior conversation.
func (d *claudeDriver) SpawnCommand(baseCommand string) string {
	d.mu.Lock()
	sid := d.claudeSessionID
	d.mu.Unlock()
	return cli.ResumeCommand(baseCommand, sid)
}

// refreshMeta folds any new transcript content into the Tracker (the
// single window through which the driver consumes JSONL) and copies the
// resulting snapshot into local fields the readers expose. No-ops when
// the path can't be resolved yet (pre-SessionStart).
//
// statusLine is cached unconditionally — formatting is microsecond-cheap
// and the value is consumed by the active-session sync path; gating it
// here would just complicate the read path for no benefit.
func (d *claudeDriver) refreshMeta() {
	d.mu.Lock()
	path := d.resolveTranscriptPathLocked()
	csid := d.claudeSessionID
	d.mu.Unlock()
	if path == "" || csid == "" {
		return
	}
	if _, err := d.tracker.Update(csid, path); err != nil {
		slog.Debug("claude driver: tracker update failed", "path", path, "err", err)
		return
	}
	snap := d.tracker.Snapshot(csid)
	line := d.tracker.StatusLine(csid)

	d.mu.Lock()
	defer d.mu.Unlock()
	d.title = snap.Title
	// Only overwrite lastPrompt when the tracker actually found a user
	// entry. snap.LastPrompt is "" briefly on a brand-new session before
	// Claude has flushed the first prompt to JSONL — clobbering would
	// erase the value we just seeded from the hook payload in HandleEvent.
	if snap.LastPrompt != "" {
		d.lastPrompt = snap.LastPrompt
	}
	d.currentTool = snap.Insight.CurrentTool
	d.subagentCounts = snap.Insight.SubagentCounts
	d.statusLine = line
}

// resolveTranscriptPathLocked picks the best known transcript path. Caller
// must hold d.mu. Priority:
//  1. Agent-reported path (canonical, handles --worktree)
//  2. Computed path from working_dir + claudeSessionID
//  3. "" if neither is available
func (d *claudeDriver) resolveTranscriptPathLocked() string {
	if d.transcriptPath != "" {
		return d.transcriptPath
	}
	if d.home == "" || d.claudeSessionID == "" || d.workingDir == "" {
		return ""
	}
	return filepath.Join(d.home, ".claude", "projects", projectDir(d.workingDir), d.claudeSessionID+".jsonl")
}

// projectDir mirrors Claude Code's encoding of working dir → ~/.claude/projects/
// dir name: replace / and . with -.
func projectDir(p string) string {
	return strings.NewReplacer("/", "-", ".", "-").Replace(p)
}

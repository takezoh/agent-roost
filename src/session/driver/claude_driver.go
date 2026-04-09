package driver

import (
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/take/agent-roost/lib/claude/cli"
	"github.com/take/agent-roost/lib/claude/transcript"
)

// Claude driver: event-driven status producer for the Claude Code CLI.
//
// All dynamic state lives in the instance and is mutated by HandleEvent
// (hook events from `roost claude event`) and refreshed (transcript meta)
// from Tick. Construction never touches I/O — RestorePersistedState is the
// only path that fills the instance from external storage.

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

	// Refresh transcript metadata every N ticks (~5 seconds at 1 Hz).
	claudeMetaRefreshTicks = 5
)

type claudeDriver struct {
	mu sync.Mutex

	// Static deps
	fsys    fs.FS  // for transcript reading
	home    string // for ~/.claude/projects/... resolution
	tracker *transcript.Tracker

	// Identity (set via RestorePersistedState or HandleEvent)
	sessionID      string
	workingDir     string
	transcriptPath string

	// Dynamic state
	status         StatusInfo
	title          string
	lastPrompt     string
	subjects       []string
	statusLine     string
	currentTool    string
	subagentCounts map[string]int
	errorCount     int
	tickCounter    int
}

func newClaudeFactory() Factory {
	return func(deps Deps) Driver {
		now := time.Now()
		return &claudeDriver{
			fsys:    deps.FS,
			home:    deps.Home,
			tracker: transcript.NewTracker(),
			status:  StatusInfo{Status: StatusIdle, ChangedAt: now},
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

// Tick: Claude is event-driven, so the only periodic work is occasional
// transcript refresh to pick up title / lastPrompt / insight changes from
// JSONL deltas. The window argument is unused (no capture-pane needed).
func (d *claudeDriver) Tick(now time.Time, _ WindowInfo) {
	d.mu.Lock()
	d.tickCounter++
	if d.tickCounter%claudeMetaRefreshTicks != 0 {
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()
	d.refreshMeta()
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
		// Trigger an immediate meta refresh: SessionStart often arrives
		// before any state-change, and we want the title chip populated
		// as soon as the transcript file appears.
		d.refreshMeta()
		return true
	case AgentEventStateChange:
		status, ok := ParseStatus(ev.State)
		if !ok {
			return false
		}
		d.mu.Lock()
		d.absorbDriverStateLocked(ev.DriverState)
		d.status = StatusInfo{Status: status, ChangedAt: time.Now()}
		d.mu.Unlock()
		d.refreshMeta()
		return true
	}
	return false
}

func (d *claudeDriver) absorbDriverStateLocked(ds map[string]string) {
	if ds == nil {
		return
	}
	if v, ok := ds[claudeKeySessionID]; ok && v != "" {
		d.sessionID = v
	}
	if v, ok := ds[claudeKeyWorkingDir]; ok && v != "" {
		d.workingDir = v
	}
	if v, ok := ds[claudeKeyTranscriptPath]; ok && v != "" {
		d.transcriptPath = v
	}
}

func (d *claudeDriver) Close() {}

func (d *claudeDriver) Status() (StatusInfo, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.status, true
}

func (d *claudeDriver) Title() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.title
}

func (d *claudeDriver) LastPrompt() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.lastPrompt
}

func (d *claudeDriver) Subjects() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.subjects) == 0 {
		return nil
	}
	out := make([]string, len(d.subjects))
	copy(out, d.subjects)
	return out
}

func (d *claudeDriver) StatusLine() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.statusLine
}

// Indicators returns the driver-formatted status chips: current tool,
// subagent counts, error count. Mirrors the legacy AgentSession.Indicators().
func (d *claudeDriver) Indicators() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []string
	if d.currentTool != "" {
		out = append(out, "▸ "+d.currentTool)
	}
	subs := 0
	for _, n := range d.subagentCounts {
		subs += n
	}
	if subs > 0 {
		out = append(out, fmt.Sprintf("%d subs", subs))
	}
	if d.errorCount > 0 {
		out = append(out, fmt.Sprintf("%d err", d.errorCount))
	}
	return out
}

// PersistedState returns the opaque bag SessionService rounds-trips through
// tmux user options + sessions.json. Includes status so warm/cold restart
// restores the prior status without resetting to Idle.
func (d *claudeDriver) PersistedState() map[string]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[string]string, 5)
	if d.sessionID != "" {
		out[claudeKeySessionID] = d.sessionID
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
	return out
}

// RestorePersistedState rehydrates identity + status from the opaque bag
// previously returned by PersistedState. Empty maps leave the factory
// defaults intact.
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
	d.mu.Unlock()
	// Pre-populate transcript meta so the UI shows the prior title/insight
	// immediately on restart, without waiting for the first periodic tick.
	d.refreshMeta()
}

// SpawnCommand returns "claude --resume <id>" when an agent session ID is
// known so cold-boot recovery picks up the prior conversation.
func (d *claudeDriver) SpawnCommand(baseCommand string) string {
	d.mu.Lock()
	sid := d.sessionID
	d.mu.Unlock()
	return cli.ResumeCommand(baseCommand, sid)
}

// refreshMeta opens the JSONL transcript and updates the cached title /
// lastPrompt / insight + statusLine. No-ops when the path can't be resolved
// yet (pre-SessionStart). Locks are held only while mutating fields.
func (d *claudeDriver) refreshMeta() {
	d.mu.Lock()
	path := d.resolveTranscriptPathLocked()
	sid := d.sessionID
	d.mu.Unlock()
	if path == "" || sid == "" {
		return
	}
	// Tracker uses fs.FS implicitly via os.Open under the hood — but our
	// tracker isn't fs.FS-aware. Use the driver's fsys for parsing meta and
	// the tracker for status-line aggregation (which calls os.Open directly).
	d.refreshTranscriptMeta(path)
	if d.tracker != nil {
		if line, changed := d.tracker.Update(sid, path); changed {
			d.mu.Lock()
			d.statusLine = line
			d.mu.Unlock()
		}
	}
}

func (d *claudeDriver) refreshTranscriptMeta(path string) {
	if d.fsys == nil {
		return
	}
	f, err := d.fsys.Open(strings.TrimPrefix(path, "/"))
	if err != nil {
		slog.Debug("claude driver: open transcript failed", "path", path, "err", err)
		return
	}
	defer f.Close()
	parser := transcript.NewParser(transcript.ParserOptions{})
	entries := parser.ParseAll(f)
	snap := transcript.AggregateMeta(entries)

	d.mu.Lock()
	defer d.mu.Unlock()
	if snap.Title != "" {
		d.title = snap.Title
	}
	if snap.LastPrompt != "" {
		d.lastPrompt = snap.LastPrompt
	}
	if len(snap.Subjects) > 0 {
		d.subjects = append(d.subjects[:0], snap.Subjects...)
	}
	d.currentTool = snap.Insight.CurrentTool
	d.subagentCounts = snap.Insight.SubagentCounts
	d.errorCount = snap.Insight.ErrorCount
}

// resolveTranscriptPathLocked picks the best known transcript path. Caller
// must hold d.mu. Priority:
//  1. Agent-reported path (canonical, handles --worktree)
//  2. Computed path from working_dir + session_id
//  3. "" if neither is available
func (d *claudeDriver) resolveTranscriptPathLocked() string {
	if d.transcriptPath != "" {
		return d.transcriptPath
	}
	if d.home == "" || d.sessionID == "" || d.workingDir == "" {
		return ""
	}
	return filepath.Join(d.home, ".claude", "projects", projectDir(d.workingDir), d.sessionID+".jsonl")
}

// projectDir mirrors Claude Code's encoding of working dir → ~/.claude/projects/
// dir name: replace / and . with -.
func projectDir(p string) string {
	return strings.NewReplacer("/", "-", ".", "-").Replace(p)
}

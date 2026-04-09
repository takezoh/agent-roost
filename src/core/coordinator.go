package core

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
	"github.com/take/agent-roost/tmux"
)

// Coordinator wires SessionService and DriverService together. The two
// services never reference each other directly — Coordinator is the only
// place that knows about both. Driver / Session correlation is by sessionID.
type Coordinator struct {
	Sessions *session.SessionService
	Drivers  *driver.DriverService
	Panes    tmux.PaneOperator
	Tmux     *tmux.Client

	SessionName    string
	eventLogDir    string
	activeWindowID string
	syncActive     func(string)
	syncStatus     func(string)
	onPreview      []func(string)
}

// NewCoordinator constructs a Coordinator. event log directory is created
// if missing.
func NewCoordinator(sessions *session.SessionService, drivers *driver.DriverService, panes tmux.PaneOperator, tmuxClient *tmux.Client, sessionName, eventLogDir, activeWindowID string) *Coordinator {
	if eventLogDir != "" {
		os.MkdirAll(eventLogDir, 0o755)
	}
	return &Coordinator{
		Sessions:       sessions,
		Drivers:        drivers,
		Panes:          panes,
		Tmux:           tmuxClient,
		SessionName:    sessionName,
		eventLogDir:    eventLogDir,
		activeWindowID: activeWindowID,
	}
}

func (c *Coordinator) SetSyncStatus(fn func(string)) { c.syncStatus = fn }
func (c *Coordinator) SetSyncActive(fn func(string)) { c.syncActive = fn }
func (c *Coordinator) OnPreview(fn func(sessionID string)) {
	c.onPreview = append(c.onPreview, fn)
}

func (c *Coordinator) emitPreview(sessionID string) {
	for _, fn := range c.onPreview {
		fn(sessionID)
	}
}

func (c *Coordinator) setActiveWindowID(wid string) {
	c.activeWindowID = wid
	if c.syncActive != nil {
		c.syncActive(wid)
	}
}

func (c *Coordinator) ActiveWindowID() string { return c.activeWindowID }

func (c *Coordinator) ClearActive(windowID string) {
	if c.activeWindowID == windowID {
		slog.Info("clear active", "window", windowID)
		c.setActiveWindowID("")
	}
}

// Preview swaps the given session into pane 0.0 without focusing it.
func (c *Coordinator) Preview(sess *session.Session) error {
	slog.Info("preview", "window", sess.WindowID)
	if err := c.Panes.RunChain(c.buildSwapChain(sess)...); err != nil {
		slog.Error("preview failed", "target", sess.WindowID, "active", c.activeWindowID, "err", err)
		return err
	}
	c.setActiveWindowID(sess.WindowID)
	c.emitPreview(sess.ID)
	return nil
}

// Switch swaps the given session into pane 0.0 and focuses it.
func (c *Coordinator) Switch(sess *session.Session) error {
	slog.Info("switch", "window", sess.WindowID)
	if err := c.Panes.RunChain(c.buildSwapChain(sess)...); err != nil {
		slog.Error("switch failed", "target", sess.WindowID, "active", c.activeWindowID, "err", err)
		return err
	}
	c.setActiveWindowID(sess.WindowID)
	return c.Panes.SelectPane(c.SessionName + ":0.0")
}

// Deactivate swaps whatever is currently in pane 0.0 back to its origin.
func (c *Coordinator) Deactivate() error {
	if c.activeWindowID == "" {
		return nil
	}
	pane0 := c.SessionName + ":0.0"
	cmd := []string{"swap-pane", "-d", "-s", pane0, "-t", c.activeWindowID + ".0"}
	if err := c.Panes.RunChain(cmd); err != nil {
		return err
	}
	c.setActiveWindowID("")
	return nil
}

func (c *Coordinator) FocusPane(pane string) {
	c.Panes.SelectPane(c.SessionName + ":" + pane)
}

func (c *Coordinator) LaunchTool(toolName string, args map[string]string) {
	slog.Info("launch tool", "tool", toolName)
	exe, _ := os.Executable()
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	paletteArgs := []string{"--tui", "palette", "--tool=" + toolName}
	for k, v := range args {
		if v != "" {
			paletteArgs = append(paletteArgs, "--arg="+k+"="+v)
		}
	}
	popupCmd := resolved + " " + strings.Join(paletteArgs, " ")
	exec.Command("tmux", "display-popup", "-E", "-w", "60%", "-h", "50%", popupCmd).Start()
}

func (c *Coordinator) ActiveSessionLogPath() string {
	if c.activeWindowID == "" {
		return ""
	}
	for _, sess := range c.Sessions.All() {
		if sess.WindowID == c.activeWindowID {
			return session.LogPath(c.Sessions.DataDir(), sess.ID)
		}
	}
	return ""
}

// Create constructs a new session: tmux window via SessionService, then a
// fresh Driver instance via DriverService. Both halves are guaranteed to
// exist together — Server handlers must call this rather than reaching
// into the services directly.
func (c *Coordinator) Create(project, command string) (*session.Session, error) {
	sess, err := c.Sessions.Create(project, command)
	if err != nil {
		return nil, err
	}
	c.Drivers.Create(sess.ID, sess.Command)
	// Persist the freshly initialized PersistedState (driver factory's
	// initial values) so warm-restart immediately after creation finds a
	// non-empty bag.
	if drv, ok := c.Drivers.Get(sess.ID); ok {
		c.Sessions.UpdatePersistedState(sess.ID, drv.PersistedState())
	}
	return sess, nil
}

// Stop kills a session window and tears down its Driver instance.
func (c *Coordinator) Stop(id string) error {
	sess, err := c.Sessions.Stop(id)
	if err != nil {
		return err
	}
	if sess != nil {
		c.Drivers.Close(sess.ID)
		c.ClearActive(sess.WindowID)
	}
	return nil
}

// Refresh is the warm-restart entry point. It rebuilds the session list
// from tmux user options and restores each session's Driver instance from
// its persisted bag.
func (c *Coordinator) Refresh() error {
	if err := c.Sessions.Refresh(); err != nil {
		return err
	}
	for _, sess := range c.Sessions.All() {
		c.Drivers.Restore(sess.ID, sess.Command, sess.PersistedState)
	}
	return nil
}

// Recreate is the cold-boot entry point. It loads sessions.json, restores
// each session's Driver from the persisted bag, asks the driver to build
// a resume command, and spawns a new tmux window for it.
func (c *Coordinator) Recreate() error {
	snapshot, err := c.Sessions.LoadSnapshot()
	if err != nil {
		return err
	}
	if len(snapshot) == 0 {
		return nil
	}
	slog.Info("recreating sessions from snapshot", "count", len(snapshot))
	for _, sess := range snapshot {
		drv := c.Drivers.Restore(sess.ID, sess.Command, sess.PersistedState)
		spawnCmd := drv.SpawnCommand(sess.Command)
		startDir := sess.Project
		if wd := sess.PersistedState["working_dir"]; wd != "" {
			startDir = wd
		}
		// SessionService.Spawn re-queries the agent pane id since tmux
		// reissues every pane id on cold boot.
		if err := c.Sessions.Spawn(sess, spawnCmd, startDir); err != nil {
			slog.Error("recreate spawn failed", "id", sess.ID, "err", err)
			c.Drivers.Close(sess.ID)
			continue
		}
	}
	return nil
}

// Tick fans out the periodic poll to every Driver. SessionService is the
// authoritative source of which sessions exist; missing drivers are skipped.
// After each Driver returns, persisted state is re-written if it changed.
func (c *Coordinator) Tick(now time.Time) {
	for _, sess := range c.Sessions.All() {
		drv, ok := c.Drivers.Get(sess.ID)
		if !ok {
			continue
		}
		win := newWindowInfoAdapter(sess, c.Tmux)
		drv.Tick(now, win)
		c.flushPersistedState(sess, drv)
	}
}

// HandleHookEvent routes a Claude (or other) hook event to the right
// Driver. The event's Pane is mapped to a sessionID via SessionService.
// Returns whether the event was consumed.
func (c *Coordinator) HandleHookEvent(ev driver.AgentEvent) (sessionID string, consumed bool) {
	sess := c.findSessionByPane(ev.Pane)
	if sess == nil {
		return "", false
	}
	drv, ok := c.Drivers.Get(sess.ID)
	if !ok {
		return sess.ID, false
	}
	consumed = drv.HandleEvent(ev)
	c.flushPersistedState(sess, drv)
	if consumed && c.syncStatus != nil && c.activeWindowID == sess.WindowID {
		c.syncStatus(drv.StatusLine())
	}
	return sess.ID, consumed
}

// findSessionByPane resolves a tmux pane id to a Session via the agent
// pane id stored at session creation. Falls back to the active session if
// the pane is empty.
func (c *Coordinator) findSessionByPane(pane string) *session.Session {
	if pane != "" {
		// Pane ids (%5) are stable across swap-pane, so SessionService
		// can find the owning Session by id.
		if sess := c.Sessions.FindByAgentPaneID(pane); sess != nil {
			return sess
		}
		// Otherwise resolve through tmux: pane → window → session.
		if wid, err := c.Panes.WindowIDFromPane(pane); err == nil {
			if sess := c.Sessions.FindByWindowID(wid); sess != nil {
				return sess
			}
		}
	}
	if c.activeWindowID == "" {
		return nil
	}
	return c.Sessions.FindByWindowID(c.activeWindowID)
}

// flushPersistedState writes the driver's current PersistedState bag back
// to SessionService if it differs from what's already there. SessionService
// short-circuits when nothing changed, so this is cheap to call after
// every Tick / HandleEvent.
func (c *Coordinator) flushPersistedState(sess *session.Session, drv driver.Driver) {
	persisted := drv.PersistedState()
	c.Sessions.UpdatePersistedState(sess.ID, persisted)
}

// SyncActiveStatusLine pushes the active session's cached status line to tmux.
func (c *Coordinator) SyncActiveStatusLine() {
	if c.syncStatus == nil {
		return
	}
	if c.activeWindowID == "" {
		c.syncStatus("")
		return
	}
	sess := c.Sessions.FindByWindowID(c.activeWindowID)
	if sess == nil {
		c.syncStatus("")
		return
	}
	if drv, ok := c.Drivers.Get(sess.ID); ok {
		c.syncStatus(drv.StatusLine())
	} else {
		c.syncStatus("")
	}
}

func (c *Coordinator) buildSwapChain(sess *session.Session) [][]string {
	pane0 := c.SessionName + ":0.0"
	var cmds [][]string
	if c.activeWindowID != "" {
		cmds = append(cmds, []string{"swap-pane", "-d", "-s", pane0, "-t", c.activeWindowID + ".0"})
	}
	cmds = append(cmds, []string{"swap-pane", "-d", "-s", pane0, "-t", sess.WindowID + ".0"})
	return cmds
}

// AppendEventLog writes a timestamped line to the session's event log file.
func (c *Coordinator) AppendEventLog(sessionID, line string) {
	if c.eventLogDir == "" || sessionID == "" {
		return
	}
	path := filepath.Join(c.eventLogDir, sessionID+".log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("15:04:05"), line)
}

// EventLogPathByWindow returns the event log file path for the active session.
func (c *Coordinator) EventLogPathByWindow(windowID string) string {
	if c.eventLogDir == "" || windowID == "" {
		return ""
	}
	sess := c.Sessions.FindByWindowID(windowID)
	if sess == nil {
		return ""
	}
	return filepath.Join(c.eventLogDir, sess.ID+".log")
}

// ActiveTranscriptPath returns the transcript file path for the active
// session, by asking its Driver. Empty if there's no active session or no
// driver claims a transcript.
func (c *Coordinator) ActiveTranscriptPath() string {
	if c.activeWindowID == "" {
		return ""
	}
	sess := c.Sessions.FindByWindowID(c.activeWindowID)
	if sess == nil {
		return ""
	}
	drv, ok := c.Drivers.Get(sess.ID)
	if !ok {
		return ""
	}
	return drv.PersistedState()["transcript_path"]
}

// ReapDeadSessions detects sessions whose tmux window has disappeared
// (agent process exited normally), evicts them from SessionService, and
// closes their Driver instances. Also handles the more delicate dead-pane
// case where the active session's agent pane has died but the window
// itself is still alive (because the agent pane is currently swapped into
// pane 0.0 and roost:0:0 is remain-on-exit).
func (c *Coordinator) ReapDeadSessions() []session.RemovedSession {
	c.reapDeadActivePane00()
	removed, err := c.Sessions.ReconcileWindows()
	if err != nil {
		slog.Error("reconcile windows failed", "err", err)
		return nil
	}
	for _, r := range removed {
		c.Drivers.Close(r.ID)
		c.ClearActive(r.WindowID)
	}
	return removed
}

// reapDeadActivePane00 looks for the case where the active session's
// agent pane (currently displayed at roost:0.0 due to swap-pane) has
// died. tmux's remain-on-exit on window 0 keeps the dead pane resident
// instead of evicting it, so the normal ReconcileWindows path can't see
// the issue. We pull the live pane id off pane 0.0, ask SessionService
// which session owns that pane id, swap it back, and kill the window.
func (c *Coordinator) reapDeadActivePane00() {
	out, err := c.Panes.DisplayMessage(c.SessionName+":0.0", "#{pane_dead} #{pane_id}")
	if err != nil {
		return
	}
	parts := strings.Fields(out)
	if len(parts) != 2 || parts[0] != "1" {
		return
	}
	deadPaneID := parts[1]
	owner := c.Sessions.FindByAgentPaneID(deadPaneID)
	if owner == nil {
		// pane 0.0 is dead but no roost session owns it. Either the main
		// TUI itself died (healthMonitor will respawn it) or roost was
		// quit. Don't touch it.
		return
	}
	slog.Info("reaping dead pane", "pane", deadPaneID, "session", owner.ID, "window", owner.WindowID)
	pane0 := c.SessionName + ":0.0"
	swap := []string{"swap-pane", "-d", "-s", pane0, "-t", owner.WindowID + ".0"}
	if err := c.Panes.RunChain(swap); err != nil {
		slog.Error("reap swap-back failed", "err", err)
		return
	}
	if err := c.Sessions.KillWindow(owner.WindowID); err != nil {
		slog.Error("reap kill-window failed", "err", err)
		return
	}
	c.Drivers.Close(owner.ID)
	c.ClearActive(owner.WindowID)
}

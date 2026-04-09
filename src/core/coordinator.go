package core

import (
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
	activeWindowID string
	syncActive     func(string)
	syncStatus     func(string)
}

// NewCoordinator constructs a Coordinator.
func NewCoordinator(sessions *session.SessionService, drivers *driver.DriverService, panes tmux.PaneOperator, tmuxClient *tmux.Client, sessionName, activeWindowID string) *Coordinator {
	return &Coordinator{
		Sessions:       sessions,
		Drivers:        drivers,
		Panes:          panes,
		Tmux:           tmuxClient,
		SessionName:    sessionName,
		activeWindowID: activeWindowID,
	}
}

func (c *Coordinator) SetSyncStatus(fn func(string)) { c.syncStatus = fn }
func (c *Coordinator) SetSyncActive(fn func(string)) { c.syncActive = fn }

func (c *Coordinator) setActiveWindowID(wid string) {
	c.activeWindowID = wid
	if c.syncActive != nil {
		c.syncActive(wid)
	}
}

func (c *Coordinator) ActiveWindowID() string { return c.activeWindowID }

// sessionContextAdapter implements driver.SessionContext for one session.
// It closes over the Coordinator and the stable sessionID — Active()
// resolves the current WindowID through SessionService at query time.
// This is robust to cold-boot pane/window id reissue, since sessionID is
// the only identifier that survives a tmux server restart.
//
// Coordinator.activeWindowID is the single source of truth — there is no
// state cached on the adapter. swap-pane changes the active window in one
// place; every Driver's next Active() call observes the new value.
type sessionContextAdapter struct {
	coord     *Coordinator
	sessionID string
}

func (a *sessionContextAdapter) Active() bool {
	if a.coord.activeWindowID == "" {
		return false
	}
	sess := a.coord.Sessions.FindByID(a.sessionID)
	if sess == nil {
		return false
	}
	return a.coord.activeWindowID == sess.WindowID
}

func (a *sessionContextAdapter) ID() string {
	return a.sessionID
}

func (c *Coordinator) sessionContextFor(sessionID string) driver.SessionContext {
	return &sessionContextAdapter{coord: c, sessionID: sessionID}
}

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

// Create constructs a new session: tmux window via SessionService, then a
// fresh Driver instance via DriverService. Both halves are guaranteed to
// exist together — Server handlers must call this rather than reaching
// into the services directly.
func (c *Coordinator) Create(project, command string) (*session.Session, error) {
	sess, err := c.Sessions.Create(project, command)
	if err != nil {
		return nil, err
	}
	c.Drivers.Create(sess.ID, sess.Command, c.sessionContextFor(sess.ID))
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
		c.Drivers.Restore(sess.ID, sess.Command, sess.PersistedState, c.sessionContextFor(sess.ID))
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
		drv := c.Drivers.Restore(sess.ID, sess.Command, sess.PersistedState, c.sessionContextFor(sess.ID))
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

// HandleHookEvent routes a Claude (or other) hook event to the right Driver.
// The event carries the roost sessionID directly (set by the hook bridge from
// $ROOST_SESSION_ID), so routing is a single FindByID lookup — no pane id
// indirection. Returns whether the event was consumed.
func (c *Coordinator) HandleHookEvent(ev driver.AgentEvent) (sessionID string, consumed bool) {
	if ev.SessionID == "" {
		slog.Warn("hook event: missing session id", "type", ev.Type, "state", ev.State)
		return "", false
	}
	sess := c.Sessions.FindByID(ev.SessionID)
	if sess == nil {
		slog.Warn("hook event: unknown session", "session", ev.SessionID, "type", ev.Type)
		return "", false
	}
	drv, ok := c.Drivers.Get(sess.ID)
	if !ok {
		slog.Warn("hook event: no driver for session", "session", ev.SessionID, "type", ev.Type)
		return sess.ID, false
	}
	consumed = drv.HandleEvent(ev)
	if !consumed {
		slog.Debug("hook event: not consumed by driver",
			"session", sess.ID, "type", ev.Type, "state", ev.State)
	}
	c.flushPersistedState(sess, drv)
	if consumed && c.syncStatus != nil && c.activeWindowID == sess.WindowID {
		c.syncStatus(drv.View().StatusLine)
	}
	return sess.ID, consumed
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
		c.syncStatus(drv.View().StatusLine)
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

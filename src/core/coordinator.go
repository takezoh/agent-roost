package core

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
	"github.com/take/agent-roost/tmux"
)

// Coordinator wires SessionService and DriverService together. The two
// services never reference each other directly — Coordinator is the only
// place that knows about both. Driver / Session correlation is by sessionID.
//
// Coordinator is implemented as an actor: a single goroutine (started by
// Start) owns SessionService, DriverService, activeWindowID, and the
// sync callbacks. Every state-touching public method routes through the
// inbox (see exec in coordinator_actor.go) so the actor goroutine is the
// only one that ever reads or writes the underlying state. Init-time
// methods (NewCoordinator, Refresh, Recreate, SetSync*, SetActiveWindowID)
// are inline-only and MUST be called before Start.
type Coordinator struct {
	Sessions *session.SessionService
	Drivers  *driver.DriverService
	Panes    tmux.PaneOperator
	Tmux     *tmux.Client

	SessionName    string
	activeWindowID string
	syncActive     func(string)
	syncStatus     func(string)

	// Actor primitives (initialized lazily on Start so init-only callers
	// who never reach Start don't pay for the channels).
	inbox     chan func()
	stop      chan struct{}
	stopped   chan struct{}
	closeOnce sync.Once

	// Server registers this callback to receive sessions-changed events
	// fired from the actor goroutine after every Tick / Reap. Must be
	// non-blocking (Server's asyncBroadcast satisfies this).
	notifySessionsChanged sessionsChangedNotifier
}

// NewCoordinator constructs a Coordinator. The activeWindowID can be set
// later via SetActiveWindowID before Start is called (warm-restart path).
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

// SetActiveWindowID is the init-only setter used during warm-restart to
// restore the previously focused window before the actor starts.
func (c *Coordinator) SetActiveWindowID(wid string) { c.activeWindowID = wid }

// SetSyncStatus / SetSyncActive register the tmux-side callbacks the
// actor calls when state changes. Init-only.
func (c *Coordinator) SetSyncStatus(fn func(string)) { c.syncStatus = fn }
func (c *Coordinator) SetSyncActive(fn func(string)) { c.syncActive = fn }

// setActiveWindowIDInternal mutates activeWindowID and notifies tmux.
// Caller must already be on the actor goroutine.
func (c *Coordinator) setActiveWindowIDInternal(wid string) {
	c.activeWindowID = wid
	if c.syncActive != nil {
		c.syncActive(wid)
	}
}

// ActiveWindowID returns the currently focused window id. Goes through
// the actor so callers always observe a consistent snapshot.
func (c *Coordinator) ActiveWindowID() string {
	var wid string
	c.exec(func() { wid = c.activeWindowID })
	return wid
}

// isActiveInternal is the actor-internal predicate used by Tick.
func (c *Coordinator) isActiveInternal(windowID string) bool {
	return c.activeWindowID != "" && c.activeWindowID == windowID
}

// ClearActive resets the active window id when the named window is the
// current one. Used by Stop / Reap paths.
func (c *Coordinator) ClearActive(windowID string) {
	c.exec(func() { c.clearActiveInternal(windowID) })
}

func (c *Coordinator) clearActiveInternal(windowID string) {
	if c.activeWindowID == windowID {
		slog.Info("clear active", "window", windowID)
		c.setActiveWindowIDInternal("")
	}
}

// Preview swaps the named session into pane 0.0 without focusing it.
func (c *Coordinator) Preview(id string) error {
	var err error
	c.exec(func() { err = c.previewInternal(id) })
	return err
}

func (c *Coordinator) previewInternal(id string) error {
	sess := c.Sessions.FindByID(id)
	if sess == nil {
		return errSessionNotFound(id)
	}
	slog.Info("preview", "window", sess.WindowID)
	if err := c.Panes.RunChain(c.buildSwapChain(sess)...); err != nil {
		slog.Error("preview failed", "target", sess.WindowID, "active", c.activeWindowID, "err", err)
		return err
	}
	c.setActiveWindowIDInternal(sess.WindowID)
	return nil
}

// Switch swaps the named session into pane 0.0 and focuses it.
func (c *Coordinator) Switch(id string) error {
	var err error
	c.exec(func() { err = c.switchInternal(id) })
	return err
}

func (c *Coordinator) switchInternal(id string) error {
	sess := c.Sessions.FindByID(id)
	if sess == nil {
		return errSessionNotFound(id)
	}
	slog.Info("switch", "window", sess.WindowID)
	if err := c.Panes.RunChain(c.buildSwapChain(sess)...); err != nil {
		slog.Error("switch failed", "target", sess.WindowID, "active", c.activeWindowID, "err", err)
		return err
	}
	c.setActiveWindowIDInternal(sess.WindowID)
	return c.Panes.SelectPane(c.SessionName + ":0.0")
}

// Deactivate swaps whatever is currently in pane 0.0 back to its origin.
func (c *Coordinator) Deactivate() error {
	var err error
	c.exec(func() { err = c.deactivateInternal() })
	return err
}

func (c *Coordinator) deactivateInternal() error {
	if c.activeWindowID == "" {
		return nil
	}
	pane0 := c.SessionName + ":0.0"
	cmd := []string{"swap-pane", "-d", "-s", pane0, "-t", c.activeWindowID + ".0"}
	if err := c.Panes.RunChain(cmd); err != nil {
		return err
	}
	c.setActiveWindowIDInternal("")
	return nil
}

// FocusPane focuses the named tmux pane. Read-only on Coordinator state
// (only delegates to Panes), but routed through the actor for ordering
// consistency with state-mutating commands.
func (c *Coordinator) FocusPane(pane string) {
	c.exec(func() { c.Panes.SelectPane(c.SessionName + ":" + pane) })
}

// LaunchTool spawns a tmux popup running the named tool. Pure side
// effect on the OS, no Coordinator state involved — but routed through
// the actor for ordering consistency.
func (c *Coordinator) LaunchTool(toolName string, args map[string]string) {
	c.exec(func() { c.launchToolInternal(toolName, args) })
}

func (c *Coordinator) launchToolInternal(toolName string, args map[string]string) {
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

// Create constructs a new session: tmux window via SessionService, then
// a fresh Driver instance via DriverService. Returns the new session id.
func (c *Coordinator) Create(project, command string) (string, error) {
	var (
		id  string
		err error
	)
	c.exec(func() { id, err = c.createInternal(project, command) })
	return id, err
}

func (c *Coordinator) createInternal(project, command string) (string, error) {
	sess, err := c.Sessions.Create(project, command)
	if err != nil {
		return "", err
	}
	c.Drivers.Create(sess.ID, sess.Command)
	if drv, ok := c.Drivers.Get(sess.ID); ok {
		c.Sessions.UpdatePersistedState(sess.ID, drv.PersistedState())
	}
	return sess.ID, nil
}

// Stop kills a session window and tears down its Driver instance.
func (c *Coordinator) Stop(id string) error {
	var err error
	c.exec(func() { err = c.stopInternal(id) })
	return err
}

func (c *Coordinator) stopInternal(id string) error {
	sess, err := c.Sessions.Stop(id)
	if err != nil {
		return err
	}
	if sess != nil {
		c.Drivers.Close(sess.ID)
		c.clearActiveInternal(sess.WindowID)
	}
	return nil
}

// Refresh rebuilds the session list from tmux user options and restores
// each session's Driver instance from its persisted bag. Init-only.
func (c *Coordinator) Refresh() error {
	if err := c.Sessions.Refresh(); err != nil {
		return err
	}
	for _, sess := range c.Sessions.All() {
		c.Drivers.Restore(sess.ID, sess.Command, sess.PersistedState)
	}
	return nil
}

// Recreate loads sessions.json, restores each session's Driver from the
// persisted bag, asks the driver to build a resume command, and spawns
// a new tmux window for it. Init-only.
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
		if err := c.Sessions.Spawn(sess, spawnCmd, startDir); err != nil {
			slog.Error("recreate spawn failed", "id", sess.ID, "err", err)
			c.Drivers.Close(sess.ID)
			continue
		}
	}
	return nil
}

// tickInternal fans the periodic tick out to every Driver. Runs only on
// the actor goroutine via handleTickInternal. The active flag is baked
// into the WindowInfo so Driver actors never have to call back into
// Coordinator.
func (c *Coordinator) tickInternal(now time.Time) {
	for _, sess := range c.Sessions.All() {
		drv, ok := c.Drivers.Get(sess.ID)
		if !ok {
			continue
		}
		win := newWindowInfoAdapter(sess, c.Tmux, c.isActiveInternal(sess.WindowID))
		drv.Tick(now, win)
		c.flushPersistedStateInternal(sess, drv)
	}
}

// HandleHookEvent routes a Claude (or other) hook event to the right
// Driver. The event carries the roost sessionID directly so routing is
// a single FindByID lookup. Returns whether the event was consumed.
func (c *Coordinator) HandleHookEvent(ev driver.AgentEvent) (sessionID string, consumed bool) {
	c.exec(func() { sessionID, consumed = c.handleHookEventInternal(ev) })
	return
}

func (c *Coordinator) handleHookEventInternal(ev driver.AgentEvent) (string, bool) {
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
	consumed := drv.HandleEvent(ev)
	if !consumed {
		slog.Debug("hook event: not consumed by driver",
			"session", sess.ID, "type", ev.Type, "state", ev.State)
	}
	c.flushPersistedStateInternal(sess, drv)
	if consumed && c.syncStatus != nil && c.activeWindowID == sess.WindowID {
		c.syncStatus(drv.View().StatusLine)
	}
	return sess.ID, consumed
}

// flushPersistedStateInternal writes the driver's current PersistedState
// bag back to SessionService. SessionService short-circuits when
// nothing changed, so this is cheap to call after every Tick / event.
func (c *Coordinator) flushPersistedStateInternal(sess *session.Session, drv driver.Driver) {
	persisted := drv.PersistedState()
	c.Sessions.UpdatePersistedState(sess.ID, persisted)
}

// SyncActiveStatusLine pushes the active session's cached status line
// to tmux. Reads internal state so it routes through the actor.
func (c *Coordinator) SyncActiveStatusLine() {
	c.exec(c.syncActiveStatusLineInternal)
}

func (c *Coordinator) syncActiveStatusLineInternal() {
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

// AllSessionInfos returns a snapshot of every session shipped as
// SessionInfo records. Replaces the older "Server reaches into
// SessionService directly + calls BuildSessionInfos" pattern.
func (c *Coordinator) AllSessionInfos() []SessionInfo {
	var infos []SessionInfo
	c.exec(func() {
		infos = BuildSessionInfos(c.Sessions.All(), c.Drivers)
	})
	return infos
}

// SnapshotSessionsAndActive returns sessions + active window in a single
// actor round-trip. Convenience for command response messages that need
// both at the same instant.
func (c *Coordinator) SnapshotSessionsAndActive() (infos []SessionInfo, active string) {
	c.exec(func() {
		infos = BuildSessionInfos(c.Sessions.All(), c.Drivers)
		active = c.activeWindowID
	})
	return
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

// reapDeadSessionsInternal detects sessions whose tmux window has
// disappeared (agent process exited normally), evicts them from
// SessionService, and closes their Driver instances. Runs on the actor
// goroutine. Returns the removed sessions so handleTickInternal can
// fire a notification.
func (c *Coordinator) reapDeadSessionsInternal() []session.RemovedSession {
	c.reapDeadActivePane00Internal()
	removed, err := c.Sessions.ReconcileWindows()
	if err != nil {
		slog.Error("reconcile windows failed", "err", err)
		return nil
	}
	for _, r := range removed {
		c.Drivers.Close(r.ID)
		c.clearActiveInternal(r.WindowID)
	}
	return removed
}

// reapDeadActivePane00Internal handles the dead-pane case where the
// active session's agent pane has died but the window itself is still
// alive (because the agent pane is currently swapped into pane 0.0 and
// roost:0:0 is remain-on-exit).
func (c *Coordinator) reapDeadActivePane00Internal() {
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
	c.clearActiveInternal(owner.WindowID)
}

// errSessionNotFound is the standard error returned when a session id
// passed to Preview/Switch/Stop does not match any known session.
type sessionNotFoundError string

func (e sessionNotFoundError) Error() string { return "session not found: " + string(e) }

func errSessionNotFound(id string) error { return sessionNotFoundError(id) }

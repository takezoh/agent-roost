package runtime

import (
	"fmt"
	"os/exec"

	"github.com/takezoh/agent-roost/tmux"
)

// RealTmuxBackend wraps a *tmux.Client into the runtime's TmuxBackend
// interface. The wrapper is intentionally thin — most methods are
// one-line passthroughs.
type RealTmuxBackend struct {
	client      *tmux.Client
	sessionName string
}

// NewRealTmuxBackend constructs a backend bound to the given tmux
// client + session name. The session name is needed for the few
// operations that take a session-scoped target string.
func NewRealTmuxBackend(client *tmux.Client) *RealTmuxBackend {
	return &RealTmuxBackend{
		client:      client,
		sessionName: client.SessionName,
	}
}

// SpawnWindow creates a new tmux window for a session and returns
// the freshly assigned window id and pane id. The pane
// id is queried separately because tmux assigns it after new-window.
func (b *RealTmuxBackend) SpawnWindow(name, command, startDir string, env map[string]string) (string, string, error) {
	args := []string{"new-window", "-d", "-t", b.sessionName + ":", "-n", name, "-P", "-F", "#{window_id}"}
	if startDir != "" {
		args = append(args, "-c", startDir)
	}
	for k, v := range env {
		args = append(args, "-e", k+"="+v)
	}
	if command != "" {
		args = append(args, command)
	}
	wid, err := b.client.Run(args...)
	if err != nil {
		return "", "", err
	}
	if err := b.client.SetOption(wid, "remain-on-exit", "off"); err != nil {
		return wid, "", err
	}
	paneID, err := b.client.Run("display-message", "-t", wid+".0", "-p", "#{pane_id}")
	if err != nil {
		return wid, "", err
	}
	// Stamp the window with @roost_id = session ID so warm-restart
	// reconciliation can map tmux windows back to sessions.json entries.
	// The session ID comes from the ROOST_SESSION_ID env var injected
	// by the reducer.
	roostID := env["ROOST_SESSION_ID"]
	if roostID == "" {
		roostID = name // fallback for non-roost windows (shouldn't happen)
	}
	if err := b.client.SetWindowUserOption(wid, "@roost_id", roostID); err != nil {
		return wid, paneID, err
	}
	return wid, paneID, nil
}

func (b *RealTmuxBackend) KillWindow(windowID string) error {
	_, err := b.client.Run("kill-window", "-t", windowID)
	return err
}

func (b *RealTmuxBackend) RunChain(ops ...[]string) error {
	return b.client.RunChain(ops...)
}

func (b *RealTmuxBackend) SelectPane(target string) error {
	_, err := b.client.Run("select-pane", "-t", target)
	return err
}

func (b *RealTmuxBackend) SetStatusLine(line string) error {
	left := " "
	if line != "" {
		left += line + " "
	}
	return b.client.SetOption(b.sessionName, "status-left", left)
}

func (b *RealTmuxBackend) SetEnv(key, value string) error {
	return b.client.SetEnv(key, value)
}

func (b *RealTmuxBackend) UnsetEnv(key string) error {
	_, err := b.client.Run("set-environment", "-t", b.sessionName, "-u", key)
	return err
}

func (b *RealTmuxBackend) PaneAlive(target string) (bool, error) {
	out, err := b.client.Run("display-message", "-t", target, "-p", "#{pane_dead}")
	if err != nil {
		return false, err
	}
	return out != "1", nil
}

func (b *RealTmuxBackend) RespawnPane(target, command string) error {
	_, err := b.client.Run("respawn-pane", "-k", "-t", target, command)
	return err
}

func (b *RealTmuxBackend) CapturePane(windowID string, nLines int) (string, error) {
	return b.client.CapturePaneLines(windowID+".0", nLines)
}

func (b *RealTmuxBackend) DetachClient() error {
	return b.client.DetachClient()
}

func (b *RealTmuxBackend) KillSession() error {
	return b.client.KillSession()
}

func (b *RealTmuxBackend) DisplayPopup(width, height, cmd string) error {
	if width == "" {
		width = "60%"
	}
	if height == "" {
		height = "50%"
	}
	c := exec.Command("tmux", "display-popup", "-E", "-w", width, "-h", height, cmd)
	return c.Start() // fire-and-forget — popup runs independently
}

// ListRoostWindows is a convenience pass-through used by the runtime
// during warm-restart reconciliation. Not part of TmuxBackend (the
// reducer doesn't need it) — only the bootstrap path calls it.
func (b *RealTmuxBackend) ListRoostWindows() ([]tmux.RoostWindow, error) {
	return b.client.ListRoostWindows()
}

// DisplayMessage runs tmux display-message for the given target and
// format string. Used by bootstrap's queryPaneID to re-query
// pane IDs after warm restart.
func (b *RealTmuxBackend) DisplayMessage(target, format string) (string, error) {
	return b.client.Run("display-message", "-t", target, "-p", format)
}

// UnsetWindowUserOption removes a window-scoped user option. Used by
// Phase 8 cleanup to remove legacy @roost_* keys.
func (b *RealTmuxBackend) UnsetWindowUserOption(windowID, key string) error {
	return b.client.UnsetWindowUserOption(windowID, key)
}

// Underlying returns the wrapped *tmux.Client. Used by main during
// startup for the operations that aren't part of TmuxBackend
// (Attach, CreateSession, SetOption on session-scoped keys).
func (b *RealTmuxBackend) Underlying() *tmux.Client { return b.client }

// errSpawn wraps a spawn error with the session name for context.
func errSpawn(name string, err error) error {
	return fmt.Errorf("tmux spawn %s: %w", name, err)
}

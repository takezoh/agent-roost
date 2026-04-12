package runtime

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/takezoh/agent-roost/lib/tmux"
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
// the freshly assigned window index (e.g. "1", "2") and an empty pane id.
// The window index is stable across server restarts (unlike window IDs)
// and is used as the key in the ROOST_W_* session env vars.
func (b *RealTmuxBackend) SpawnWindow(name, command, startDir string, env map[string]string) (string, string, error) {
	args := []string{"new-window", "-d", "-t", b.sessionName + ":", "-n", name, "-P", "-F", "#{window_index}"}
	if startDir != "" {
		args = append(args, "-c", startDir)
	}
	for k, v := range env {
		args = append(args, "-e", k+"="+v)
	}
	if command != "" {
		args = append(args, command)
	}
	idx, err := b.client.Run(args...)
	if err != nil {
		return "", "", err
	}
	if err := b.client.SetOption(b.sessionName+":"+idx, "remain-on-exit", "off"); err != nil {
		return idx, "", err
	}
	return idx, "", nil
}

func (b *RealTmuxBackend) KillWindow(target string) error {
	_, err := b.client.Run("kill-window", "-t", b.sessionName+":"+target)
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

func (b *RealTmuxBackend) CapturePane(windowTarget string, nLines int) (string, error) {
	return b.client.CapturePaneLines(b.sessionName+":"+windowTarget+".0", nLines)
}

func (b *RealTmuxBackend) InspectPane(target string, nLines int) (PaneSnapshot, error) {
	meta, err := b.client.DisplayMessage(target, "#{pane_dead}\t#{pane_in_mode}\t#{pane_current_command}\t#{cursor_x}\t#{cursor_y}")
	if err != nil {
		return PaneSnapshot{}, err
	}
	content, err := b.client.CapturePaneLines(target, nLines)
	if err != nil {
		return PaneSnapshot{}, err
	}
	parts := strings.SplitN(meta, "\t", 5)
	snap := PaneSnapshot{
		Target:      target,
		ContentTail: content,
	}
	if len(parts) > 0 {
		snap.Dead = parts[0] == "1"
	}
	if len(parts) > 1 {
		snap.InMode = parts[1] == "1"
	}
	if len(parts) > 2 {
		snap.CurrentCommand = parts[2]
	}
	if len(parts) > 3 {
		snap.CursorX = parts[3]
	}
	if len(parts) > 4 {
		snap.CursorY = parts[4]
	}
	return snap, nil
}

// ListWindowIndexes returns the live window indexes for reconciliation.
func (b *RealTmuxBackend) ListWindowIndexes() ([]string, error) {
	return b.client.ListWindowIndexes()
}

// ShowEnvironment returns the raw tmux show-environment output for the
// session, used by LoadWindowMap to reconstruct the ROOST_W_* entries.
func (b *RealTmuxBackend) ShowEnvironment() (string, error) {
	return b.client.Run("show-environment", "-t", b.sessionName)
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

// Underlying returns the wrapped *tmux.Client. Used by main during
// startup for the operations that aren't part of TmuxBackend
// (Attach, CreateSession, SetOption on session-scoped keys).
func (b *RealTmuxBackend) Underlying() *tmux.Client { return b.client }

// errSpawn wraps a spawn error with the session name for context.
func errSpawn(name string, err error) error {
	return fmt.Errorf("tmux spawn %s: %w", name, err)
}

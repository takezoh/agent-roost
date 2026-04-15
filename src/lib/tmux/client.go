package tmux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// RoostWindow is a minimal snapshot of a roost-managed tmux window.
// Only WindowID and ID are populated by ListRoostWindows. All other
// session state lives in sessions.json.
type RoostWindow struct {
	WindowID string // tmux window id, e.g. "@5"
	ID       string // roost session id (from @roost_id user option)
}

type Client struct {
	SessionName    string
	defaultTimeout time.Duration // tmux shell-out timeout; 0 means use 2 seconds
}

type WindowInfo struct {
	ID     string
	Name   string
	Active bool
}

func NewClient(sessionName string) *Client {
	return &Client{
		SessionName:    sessionName,
		defaultTimeout: 2 * time.Second,
	}
}

// RunContext executes a tmux command with the provided context.
func (c *Client) RunContext(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "tmux", args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Run executes a tmux command with a default 2-second timeout.
// tmux commands complete in milliseconds; a multi-second delay means the
// server is dead and the call should fail fast rather than block forever.
func (c *Client) Run(args ...string) (string, error) {
	timeout := c.defaultTimeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return c.RunContext(ctx, args...)
}

func (c *Client) SessionExists() bool {
	_, err := c.Run("has-session", "-t", c.SessionName)
	return err == nil
}

func (c *Client) KillSession() error {
	_, err := c.Run("kill-session", "-t", c.SessionName)
	return err
}

func (c *Client) DetachClient() error {
	_, err := c.Run("detach-client", "-s", c.SessionName)
	return err
}

func (c *Client) SetEnv(key, value string) error {
	_, err := c.Run("set-environment", "-t", c.SessionName, key, value)
	return err
}

func (c *Client) GetEnv(key string) (string, error) {
	out, err := c.Run("show-environment", "-t", c.SessionName, key)
	if err != nil {
		return "", err
	}
	if parts := strings.SplitN(out, "=", 2); len(parts) == 2 {
		return parts[1], nil
	}
	return "", nil
}

func (c *Client) CreateSession(width, height int) error {
	args := []string{"new-session", "-d", "-s", c.SessionName}
	if width > 0 && height > 0 {
		args = append(args, "-x", fmt.Sprintf("%d", width), "-y", fmt.Sprintf("%d", height))
	}
	_, err := c.Run(args...)
	return err
}

func (c *Client) Attach() error {
	cmd := exec.Command("tmux", "attach-session", "-t", c.SessionName)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (c *Client) ListWindows() ([]WindowInfo, error) {
	out, err := c.Run("list-windows", "-t", c.SessionName, "-F", "#{window_id}\t#{window_name}\t#{window_active}")
	if err != nil {
		return nil, err
	}
	var windows []WindowInfo
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		windows = append(windows, WindowInfo{
			ID:     parts[0],
			Name:   parts[1],
			Active: parts[2] == "1",
		})
	}
	return windows, nil
}

func (c *Client) ListWindowIDs() ([]string, error) {
	windows, err := c.ListWindows()
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(windows))
	for i, w := range windows {
		ids[i] = w.ID
	}
	return ids, nil
}

// ListWindowIndexes returns the numeric window indexes ("0", "1", "2", ...)
// for all windows in the session. Used for reconciliation against ROOST_W_*
// env vars.
func (c *Client) ListWindowIndexes() ([]string, error) {
	out, err := c.Run("list-windows", "-t", c.SessionName, "-F", "#{window_index}")
	if err != nil {
		return nil, err
	}
	var indexes []string
	for _, line := range strings.Split(out, "\n") {
		if line != "" {
			indexes = append(indexes, line)
		}
	}
	return indexes, nil
}

func (c *Client) RunChain(commands ...[]string) error {
	var args []string
	for i, cmd := range commands {
		if i > 0 {
			args = append(args, ";")
		}
		args = append(args, cmd...)
	}
	_, err := c.Run(args...)
	return err
}

// BindKey executes a tmux bind-key command with typed arguments.
// table is the key table (e.g. "prefix"), key is the key name,
// and args are the bind-key arguments (the command to run on press).
func (c *Client) BindKey(table, key string, args ...string) error {
	a := []string{"bind-key", "-T", table, key}
	a = append(a, args...)
	_, err := c.Run(a...)
	return err
}

// UnbindAllKeys removes all key bindings from the given table.
func (c *Client) UnbindAllKeys(table string) error {
	_, err := c.Run("unbind-key", "-a", "-T", table)
	return err
}

// ShowOption returns the value of a tmux server-global option.
// Wraps `tmux show-option -gv <key>`. Returns empty string if the
// option is unset or if the server is not running.
func (c *Client) ShowOption(key string) (string, error) {
	out, err := c.Run("show-option", "-gv", key)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\n"), nil
}

func (c *Client) SetOption(target, key, value string) error {
	_, err := c.Run("set-option", "-t", target, key, value)
	return err
}

// SetWindowUserOption sets a window-scoped user option (key must start with '@').
func (c *Client) SetWindowUserOption(windowID, key, value string) error {
	_, err := c.Run("set-option", "-w", "-t", windowID, key, value)
	return err
}

// UnsetWindowUserOption removes a window-scoped user option.
func (c *Client) UnsetWindowUserOption(windowID, key string) error {
	_, err := c.Run("set-option", "-w", "-u", "-t", windowID, key)
	return err
}

// SetWindowUserOptions sets multiple window user options atomically.
func (c *Client) SetWindowUserOptions(windowID string, kv map[string]string) error {
	if len(kv) == 0 {
		return nil
	}
	cmds := make([][]string, 0, len(kv))
	for k, v := range kv {
		cmds = append(cmds, []string{"set-option", "-w", "-t", windowID, k, v})
	}
	return c.RunChain(cmds...)
}

// ListRoostWindows returns all windows that carry the @roost_id user option.
// After Phase 8, @roost_id is the ONLY roost user option — all other state
// lives in sessions.json. The returned RoostWindow only has WindowID and ID
// populated; other fields are empty.
func (c *Client) ListRoostWindows() ([]RoostWindow, error) {
	fmtStr := "#{window_id}\t#{@roost_id}"
	out, err := c.Run("list-windows", "-t", c.SessionName, "-F", fmtStr)
	if err != nil {
		return nil, err
	}
	return parseRoostWindows(out), nil
}

func parseRoostWindows(out string) []RoostWindow {
	var windows []RoostWindow
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) < 2 || parts[1] == "" {
			continue
		}
		windows = append(windows, RoostWindow{
			WindowID: parts[0],
			ID:       parts[1],
		})
	}
	return windows
}

// CapturePaneLines captures the last n lines of the given pane. Used by
// genericDriver.Tick for capture-pane based status detection — Claude
// sessions are event-driven and never hit this.
func (c *Client) CapturePaneLines(paneTarget string, n int) (string, error) {
	return c.Run("capture-pane", "-p", "-t", paneTarget, "-S", fmt.Sprintf("-%d", n))
}

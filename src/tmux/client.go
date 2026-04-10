package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RoostWindow is a raw snapshot of a roost-managed tmux window's user
// options. All fields are still in their tmux string form; the
// runtime decodes them into state.Session values. Defined in the
// tmux package so callers can read the raw layout without importing
// any business-logic packages.
type RoostWindow struct {
	WindowID       string
	ID             string
	Project        string
	Command        string
	CreatedAt      string
	AgentPaneID    string
	PersistedState string // JSON-encoded map[string]string
}

type Client struct {
	SessionName string
}

type WindowInfo struct {
	ID     string
	Name   string
	Active bool
}

func NewClient(sessionName string) *Client {
	return &Client{SessionName: sessionName}
}

func (c *Client) Run(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (c *Client) SessionExists() bool {
	cmd := exec.Command("tmux", "has-session", "-t", c.SessionName)
	return cmd.Run() == nil
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

// Deprecated: BindKeyRaw passes a raw string to sh -c "tmux ...".
// Prefer BindKey for new code — it avoids shell injection risks.
func (c *Client) BindKeyRaw(rawCmd string) error {
	cmd := exec.Command("sh", "-c", "tmux "+rawCmd)
	return cmd.Run()
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
// Driver-defined persistent state is packed into a single JSON-encoded
// @roost_persisted_state user option so this layer never has to know about
// individual driver keys. Tags are no longer stored as a top-level user
// option — drivers cache them inside their own PersistedState bag.
func (c *Client) ListRoostWindows() ([]RoostWindow, error) {
	fmtStr := strings.Join([]string{
		"#{window_id}",
		"#{@roost_id}",
		"#{@roost_project}",
		"#{@roost_command}",
		"#{@roost_created_at}",
		"#{@roost_agent_pane}",
		"#{@roost_persisted_state}",
	}, "\t")
	out, err := c.Run("list-windows", "-t", c.SessionName, "-F", fmtStr)
	if err != nil {
		return nil, err
	}
	return parseRoostWindows(out), nil
}

// parseRoostWindows parses the tab-separated output of list-windows into
// roost window snapshots. Lines whose @roost_id field is empty are skipped.
//
// The output is right-padded to roostWindowFields columns before parsing
// because Client.Run trims trailing whitespace from the tmux output, which
// silently drops empty trailing fields (e.g. an empty @roost_persisted_state
// on the last line). Without this padding, ReconcileWindows would treat
// freshly created sessions whose persisted state is still empty as "missing"
// and evict them from the cache on the next polling tick.
const roostWindowFields = 7

func parseRoostWindows(out string) []RoostWindow {
	var windows []RoostWindow
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		for len(parts) < roostWindowFields {
			parts = append(parts, "")
		}
		if parts[1] == "" {
			continue
		}
		windows = append(windows, RoostWindow{
			WindowID:       parts[0],
			ID:             parts[1],
			Project:        parts[2],
			Command:        parts[3],
			CreatedAt:      parts[4],
			AgentPaneID:    parts[5],
			PersistedState: parts[6],
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

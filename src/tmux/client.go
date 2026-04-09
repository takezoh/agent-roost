package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/take/agent-roost/session"
)

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

func (c *Client) BindKeyRaw(rawCmd string) error {
	cmd := exec.Command("sh", "-c", "tmux "+rawCmd)
	return cmd.Run()
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
// Driver-specific persistent state is packed into a single JSON-encoded
// @roost_driver_state user option so this layer never has to know about
// individual driver keys. Dynamic per-session status is owned by state.Store
// (see ListWindowOptions for the read path) — Manager only receives the
// static metadata here.
func (c *Client) ListRoostWindows() ([]session.RoostWindow, error) {
	fmtStr := strings.Join([]string{
		"#{window_id}",
		"#{@roost_id}",
		"#{@roost_project}",
		"#{@roost_command}",
		"#{@roost_created_at}",
		"#{@roost_tags}",
		"#{@roost_agent_pane}",
		"#{@roost_driver_state}",
	}, "\t")
	out, err := c.Run("list-windows", "-t", c.SessionName, "-F", fmtStr)
	if err != nil {
		return nil, err
	}
	return parseRoostWindows(out), nil
}

// parseRoostWindows parses the tab-separated output of list-windows into roost
// window snapshots. Lines whose @roost_id field is empty are skipped.
//
// The output is right-padded to roostWindowFields columns before parsing
// because Client.Run trims trailing whitespace from the tmux output, which
// silently drops empty trailing fields (e.g. an empty @roost_driver_state on
// the last line). Without this padding, ReconcileWindows would treat freshly
// created sessions whose DriverState is still empty as "missing" and evict
// them from the Manager cache on the next polling tick.
const roostWindowFields = 8

func parseRoostWindows(out string) []session.RoostWindow {
	var windows []session.RoostWindow
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
		windows = append(windows, session.RoostWindow{
			WindowID:    parts[0],
			ID:          parts[1],
			Project:     parts[2],
			Command:     parts[3],
			CreatedAt:   parts[4],
			Tags:        parts[5],
			AgentPaneID: parts[6],
			DriverState: parts[7],
		})
	}
	return windows
}

// ListWindowOptions returns the @roost_state / @roost_state_changed_at
// values for every roost-managed window, keyed by tmux window ID. Used by
// state.Store at Coordinator startup to seed itself from tmux user options
// (warm restart). Windows that are not roost-managed (no @roost_id) are
// skipped.
func (c *Client) ListWindowOptions() (map[string]map[string]string, error) {
	fmtStr := strings.Join([]string{
		"#{window_id}",
		"#{@roost_id}",
		"#{@roost_state}",
		"#{@roost_state_changed_at}",
	}, "\t")
	out, err := c.Run("list-windows", "-t", c.SessionName, "-F", fmtStr)
	if err != nil {
		return nil, err
	}
	result := make(map[string]map[string]string)
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		// Right-pad to 4 columns: Client.Run trims trailing whitespace from
		// the tmux output, which silently drops empty trailing fields (e.g.
		// @roost_state_changed_at on a session that was just spawned and
		// hasn't recorded a changed-at timestamp yet).
		for len(parts) < 4 {
			parts = append(parts, "")
		}
		if parts[1] == "" {
			continue
		}
		result[parts[0]] = map[string]string{
			"@roost_state":            parts[2],
			"@roost_state_changed_at": parts[3],
		}
	}
	return result, nil
}

// UnsetWindowUserOptions removes one or more user options from a window in
// a single tmux invocation. Used by state.Store.Delete to clear persisted
// status when a session is removed.
func (c *Client) UnsetWindowUserOptions(windowID string, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	cmds := make([][]string, 0, len(keys))
	for _, k := range keys {
		cmds = append(cmds, []string{"set-option", "-w", "-u", "-t", windowID, k})
	}
	return c.RunChain(cmds...)
}

// CapturePaneLines captures the last n lines of the given pane. Used by
// genericObserver.Tick for capture-pane based status detection — the only
// production caller. Claude sessions are event-driven and never hit this.
func (c *Client) CapturePaneLines(paneTarget string, n int) (string, error) {
	return c.Run("capture-pane", "-p", "-t", paneTarget, "-S", fmt.Sprintf("-%d", n))
}

package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
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

func (c *Client) CapturePane(paneTarget string) (string, error) {
	return c.Run("capture-pane", "-p", "-t", paneTarget)
}

func (c *Client) CapturePaneLines(paneTarget string, n int) (string, error) {
	return c.Run("capture-pane", "-p", "-t", paneTarget, "-S", fmt.Sprintf("-%d", n))
}

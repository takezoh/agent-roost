package tmux

import "fmt"

func (c *Client) SplitWindow(target string, horizontal bool, percent int) error {
	direction := "-v"
	if horizontal {
		direction = "-h"
	}
	_, err := c.Run("split-window", direction, "-l", fmt.Sprintf("%d%%", percent), "-t", target, "-d")
	return err
}

func (c *Client) SelectPane(target string) error {
	_, err := c.Run("select-pane", "-t", target)
	return err
}

func (c *Client) SwapPane(src, dst string) error {
	_, err := c.Run("swap-pane", "-d", "-s", src, "-t", dst)
	return err
}

func (c *Client) RespawnPane(target, command string) error {
	_, err := c.Run("respawn-pane", "-k", "-t", target, command)
	return err
}

func (c *Client) ResizePane(target string, widthPct, heightPct int) error {
	args := []string{"resize-pane", "-t", target}
	if widthPct > 0 {
		args = append(args, "-x", fmt.Sprintf("%d%%", widthPct))
	}
	if heightPct > 0 {
		args = append(args, "-y", fmt.Sprintf("%d%%", heightPct))
	}
	_, err := c.Run(args...)
	return err
}

func (c *Client) NewWindow(name, command, startDir string) (string, error) {
	return c.Run("new-window", "-d", "-a", "-t", c.SessionName+":", "-n", name, "-c", startDir, "-P", "-F", "#{window_id}", command)
}

func (c *Client) KillWindow(windowID string) error {
	_, err := c.Run("kill-window", "-t", windowID)
	return err
}

func (c *Client) SendKeys(target, keys string) error {
	_, err := c.Run("send-keys", "-t", target, keys, "Enter")
	return err
}

func (c *Client) PipePane(target, command string) error {
	_, err := c.Run("pipe-pane", "-t", target, command)
	return err
}

// WindowIDFromPane returns the tmux window ID for a given pane ID (e.g. "%5" → "@3").
func (c *Client) WindowIDFromPane(paneID string) (string, error) {
	return c.Run("display-message", "-t", paneID, "-p", "#{window_id}")
}

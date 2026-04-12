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

func (c *Client) BreakPane(src, dstWindow string) error {
	args := []string{"break-pane", "-d", "-s", src}
	if dstWindow != "" {
		args = append(args, "-t", dstWindow)
	}
	_, err := c.Run(args...)
	return err
}

func (c *Client) BreakPaneToNewWindow(src, name string) (string, error) {
	args := []string{"break-pane", "-d", "-s", src, "-P", "-F", "#{window_index}"}
	if name != "" {
		args = append(args, "-n", name)
	}
	return c.Run(args...)
}

func (c *Client) JoinPane(src, dst string, before bool, sizePct int) error {
	args := []string{"join-pane", "-d", "-s", src, "-t", dst}
	if before {
		args = append(args, "-b")
	}
	if sizePct > 0 {
		args = append(args, "-l", fmt.Sprintf("%d%%", sizePct))
	}
	_, err := c.Run(args...)
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

func (c *Client) NewWindow(name, command, startDir string, env map[string]string) (string, error) {
	args := []string{"new-window", "-d", "-a", "-t", c.SessionName + ":", "-n", name, "-c", startDir, "-P", "-F", "#{window_id}"}
	for k, v := range env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, command)
	return c.Run(args...)
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

// DisplayMessage runs `tmux display-message -t <target> -p <format>` and
// returns the formatted output (typically a single field like "#{pane_dead}").
func (c *Client) DisplayMessage(target, format string) (string, error) {
	return c.Run("display-message", "-t", target, "-p", format)
}

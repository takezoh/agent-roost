package proto

import (
	"context"
	"errors"
	"time"
)

// Convenience wrappers around Client.Send for the typed commands
// callers issue most often. Each helper bounds the wait with a
// short default timeout that's plenty for the local Unix socket
// roundtrip the daemon expects.

const defaultRequestTimeout = 5 * time.Second

func (c *Client) sendDefault(cmd Command) (Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultRequestTimeout)
	defer cancel()
	return c.Send(ctx, cmd)
}

// Subscribe registers this client to receive broadcast events.
func (c *Client) Subscribe() error {
	_, err := c.sendDefault(CmdSubscribe{})
	return err
}

// CreateSession asks the daemon to spawn a new session. Returns
// the freshly assigned session id and window id, or an error.
func (c *Client) CreateSession(project, command string) (sessionID, windowID string, err error) {
	resp, err := c.sendDefault(CmdCreateSession{Project: project, Command: command})
	if err != nil {
		return "", "", err
	}
	r, ok := resp.(RespCreateSession)
	if !ok {
		return "", "", errors.New("proto: unexpected response type for create-session")
	}
	return r.SessionID, r.WindowID, nil
}

// StopSession kills a session by id.
func (c *Client) StopSession(id string) error {
	_, err := c.sendDefault(CmdStopSession{SessionID: id})
	return err
}

// ListSessions returns the current session table + active window id.
func (c *Client) ListSessions() ([]SessionInfo, string, error) {
	resp, err := c.sendDefault(CmdListSessions{})
	if err != nil {
		return nil, "", err
	}
	r, ok := resp.(RespSessions)
	if !ok {
		return nil, "", errors.New("proto: unexpected response type for list-sessions")
	}
	return r.Sessions, r.ActiveWindowID, nil
}

// PreviewSession swaps a session into pane 0.0 without focusing it.
func (c *Client) PreviewSession(sessionID string) (string, error) {
	resp, err := c.sendDefault(CmdPreviewSession{SessionID: sessionID})
	if err != nil {
		return "", err
	}
	if r, ok := resp.(RespActiveWindow); ok {
		return r.ActiveWindowID, nil
	}
	return "", nil
}

// SwitchSession swaps a session into pane 0.0 and focuses it.
func (c *Client) SwitchSession(sessionID string) (string, error) {
	resp, err := c.sendDefault(CmdSwitchSession{SessionID: sessionID})
	if err != nil {
		return "", err
	}
	if r, ok := resp.(RespActiveWindow); ok {
		return r.ActiveWindowID, nil
	}
	return "", nil
}

// PreviewProject deactivates the current session and broadcasts
// project-selected.
func (c *Client) PreviewProject(project string) error {
	_, err := c.sendDefault(CmdPreviewProject{Project: project})
	return err
}

// FocusPane focuses the named control pane.
func (c *Client) FocusPane(pane string) error {
	_, err := c.sendDefault(CmdFocusPane{Pane: pane})
	return err
}

// LaunchTool tells the daemon to display a popup running the named
// tool. Args are pre-filled values the popup should start with.
func (c *Client) LaunchTool(toolName string, args map[string]string) error {
	_, err := c.sendDefault(CmdLaunchTool{Tool: toolName, Args: args})
	return err
}

// Shutdown tells the daemon to terminate.
func (c *Client) Shutdown() error {
	_, err := c.sendDefault(CmdShutdown{})
	return err
}

// Detach asks the daemon to detach the tmux client (keeps daemon alive).
func (c *Client) Detach() error {
	_, err := c.sendDefault(CmdDetach{})
	return err
}

// SendHook ships a typed hook payload from the bridge to the daemon.
// Caller picks the timeout — hook bridges typically use a short bound
// since they should not stall claude-side hook execution.
func (c *Client) SendHook(driverName, eventName, sessionID string, payload map[string]any) error {
	return c.SendNoWaitWithTimeout(CmdHook{
		Driver:    driverName,
		Event:     eventName,
		SessionID: sessionID,
		Payload:   payload,
	}, defaultRequestTimeout)
}

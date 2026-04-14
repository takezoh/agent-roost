package proto

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/takezoh/agent-roost/state"
)

const defaultRequestTimeout = 5 * time.Second

func (c *Client) sendDefault(cmd Command) (Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultRequestTimeout)
	defer cancel()
	return c.Send(ctx, cmd)
}

func sendJSONEvent[R Response](c *Client, eventName string, req any) (R, error) {
	var payload json.RawMessage
	if req != nil {
		b, err := json.Marshal(req)
		if err != nil {
			var zero R
			return zero, err
		}
		payload = json.RawMessage(b)
	}
	resp, err := c.sendDefault(CmdEvent{Event: eventName, Payload: payload})
	if err != nil {
		var zero R
		return zero, err
	}
	r, ok := resp.(R)
	if !ok {
		var zero R
		return zero, fmt.Errorf("proto: unexpected response type for %s", eventName)
	}
	return r, nil
}

// Subscribe registers this client to receive broadcast events.
func (c *Client) Subscribe() error {
	_, err := c.sendDefault(CmdSubscribe{})
	return err
}

// CreateSession asks the daemon to spawn a new session. Returns
// the freshly assigned session id, or an error.
func (c *Client) CreateSession(project, command string, options state.LaunchOptions) (sessionID string, err error) {
	r, err := sendJSONEvent[RespCreateSession](c, state.EventCreateSession, state.CreateSessionParams{
		Project: project,
		Command: command,
		Options: options,
	})
	if err != nil {
		return "", err
	}
	return r.SessionID, nil
}

// StopSession kills a session by id.
func (c *Client) StopSession(id string) error {
	_, err := sendJSONEvent[RespOK](c, state.EventStopSession, map[string]string{"session_id": id})
	return err
}

// ListSessions returns the current session table, active session id,
// connector info, and the list of enabled runtime feature flags.
func (c *Client) ListSessions() ([]SessionInfo, string, []ConnectorInfo, []string, error) {
	r, err := sendJSONEvent[RespSessions](c, state.EventListSessions, nil)
	if err != nil {
		return nil, "", nil, nil, err
	}
	return r.Sessions, r.ActiveSessionID, r.Connectors, r.Features, nil
}

// PreviewSession swaps a session into pane 0.0 without focusing it.
// Returns the active session id.
func (c *Client) PreviewSession(sessionID string) (string, error) {
	r, err := sendJSONEvent[RespActiveSession](c, state.EventPreviewSession, map[string]string{"session_id": sessionID})
	if err != nil {
		return "", err
	}
	return r.ActiveSessionID, nil
}

// SwitchSession swaps a session into pane 0.0 and focuses it.
// Returns the active session id.
func (c *Client) SwitchSession(sessionID string) (string, error) {
	r, err := sendJSONEvent[RespActiveSession](c, state.EventSwitchSession, map[string]string{"session_id": sessionID})
	if err != nil {
		return "", err
	}
	return r.ActiveSessionID, nil
}

// PreviewProject deactivates the current session and broadcasts
// project-selected.
func (c *Client) PreviewProject(project string) error {
	_, err := sendJSONEvent[RespOK](c, state.EventPreviewProject, map[string]string{"project": project})
	return err
}

// FocusPane focuses the named control pane.
func (c *Client) FocusPane(pane string) error {
	_, err := sendJSONEvent[RespOK](c, state.EventFocusPane, map[string]string{"pane": pane})
	return err
}

// LaunchTool tells the daemon to display a popup running the named
// tool. Args are pre-filled values the popup should start with.
func (c *Client) LaunchTool(toolName string, args map[string]string) error {
	m := map[string]string{"tool": toolName}
	for k, v := range args {
		m[k] = v
	}
	_, err := sendJSONEvent[RespOK](c, state.EventLaunchTool, m)
	return err
}

// Shutdown tells the daemon to terminate.
func (c *Client) Shutdown() error {
	_, err := sendJSONEvent[RespOK](c, state.EventShutdown, nil)
	return err
}

// Detach asks the daemon to detach the tmux client (keeps daemon alive).
func (c *Client) Detach() error {
	_, err := sendJSONEvent[RespOK](c, state.EventDetach, nil)
	return err
}

// PushDriver asks the daemon to push a new driver frame onto the active
// session. SessionID is left empty so the daemon uses the active session.
func (c *Client) PushDriver(command string) error {
	_, err := sendJSONEvent[RespOK](c, state.EventPushDriver, state.PushDriverParams{
		Command: command,
	})
	return err
}

// SendEvent ships a generic event to the daemon.
func (c *Client) SendEvent(eventName string, timestamp time.Time, senderID string, payload json.RawMessage) error {
	return c.SendWithTimeout(CmdEvent{
		Event:     eventName,
		Timestamp: timestamp,
		SenderID:  senderID,
		Payload:   payload,
	}, defaultRequestTimeout)
}

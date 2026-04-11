package proto

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/takezoh/agent-roost/state"
)

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
// the freshly assigned session id, or an error.
func (c *Client) CreateSession(project, command string) (sessionID string, err error) {
	payload, _ := json.Marshal(map[string]string{"project": project, "command": command})
	resp, err := c.sendDefault(CmdEvent{Event: state.EventCreateSession, Payload: json.RawMessage(payload)})
	if err != nil {
		return "", err
	}
	r, ok := resp.(RespCreateSession)
	if !ok {
		return "", errors.New("proto: unexpected response type for create-session")
	}
	return r.SessionID, nil
}

// StopSession kills a session by id.
func (c *Client) StopSession(id string) error {
	payload, _ := json.Marshal(map[string]string{"session_id": id})
	_, err := c.sendDefault(CmdEvent{Event: state.EventStopSession, Payload: json.RawMessage(payload)})
	return err
}

// ListSessions returns the current session table, active session id,
// and connector info.
func (c *Client) ListSessions() ([]SessionInfo, string, []ConnectorInfo, error) {
	resp, err := c.sendDefault(CmdEvent{Event: state.EventListSessions})
	if err != nil {
		return nil, "", nil, err
	}
	r, ok := resp.(RespSessions)
	if !ok {
		return nil, "", nil, errors.New("proto: unexpected response type for list-sessions")
	}
	return r.Sessions, r.ActiveSessionID, r.Connectors, nil
}

// PreviewSession swaps a session into pane 0.0 without focusing it.
// Returns the active session id.
func (c *Client) PreviewSession(sessionID string) (string, error) {
	payload, _ := json.Marshal(map[string]string{"session_id": sessionID})
	resp, err := c.sendDefault(CmdEvent{Event: state.EventPreviewSession, Payload: json.RawMessage(payload)})
	if err != nil {
		return "", err
	}
	if r, ok := resp.(RespActiveSession); ok {
		return r.ActiveSessionID, nil
	}
	return "", nil
}

// SwitchSession swaps a session into pane 0.0 and focuses it.
// Returns the active session id.
func (c *Client) SwitchSession(sessionID string) (string, error) {
	payload, _ := json.Marshal(map[string]string{"session_id": sessionID})
	resp, err := c.sendDefault(CmdEvent{Event: state.EventSwitchSession, Payload: json.RawMessage(payload)})
	if err != nil {
		return "", err
	}
	if r, ok := resp.(RespActiveSession); ok {
		return r.ActiveSessionID, nil
	}
	return "", nil
}

// PreviewProject deactivates the current session and broadcasts
// project-selected.
func (c *Client) PreviewProject(project string) error {
	payload, _ := json.Marshal(map[string]string{"project": project})
	_, err := c.sendDefault(CmdEvent{Event: state.EventPreviewProject, Payload: json.RawMessage(payload)})
	return err
}

// FocusPane focuses the named control pane.
func (c *Client) FocusPane(pane string) error {
	payload, _ := json.Marshal(map[string]string{"pane": pane})
	_, err := c.sendDefault(CmdEvent{Event: state.EventFocusPane, Payload: json.RawMessage(payload)})
	return err
}

// LaunchTool tells the daemon to display a popup running the named
// tool. Args are pre-filled values the popup should start with.
func (c *Client) LaunchTool(toolName string, args map[string]string) error {
	m := map[string]string{"tool": toolName}
	for k, v := range args {
		m[k] = v
	}
	payload, _ := json.Marshal(m)
	_, err := c.sendDefault(CmdEvent{Event: state.EventLaunchTool, Payload: json.RawMessage(payload)})
	return err
}

// Shutdown tells the daemon to terminate.
func (c *Client) Shutdown() error {
	_, err := c.sendDefault(CmdEvent{Event: state.EventShutdown})
	return err
}

// Detach asks the daemon to detach the tmux client (keeps daemon alive).
func (c *Client) Detach() error {
	_, err := c.sendDefault(CmdEvent{Event: state.EventDetach})
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

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
// active occupant kind, connector info, and the list of enabled runtime
// feature flags.
func (c *Client) ListSessions() ([]SessionInfo, string, string, []ConnectorInfo, []string, error) {
	r, err := sendJSONEvent[RespSessions](c, state.EventListSessions, nil)
	if err != nil {
		return nil, "", "", nil, nil, err
	}
	return r.Sessions, r.ActiveSessionID, r.ActiveOccupant, r.Connectors, r.Features, nil
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

// ActivateFrame switches the active frame for a session. The main pane
// swaps to the target frame's tmux pane.
func (c *Client) ActivateFrame(sessionID, frameID string) error {
	_, err := sendJSONEvent[RespOK](c, state.EventActivateFrame, state.ActivateFrameParams{
		SessionID: sessionID,
		FrameID:   frameID,
	})
	return err
}

// PushDriver asks the daemon to push a new driver frame onto the given session.
// input is the stdin content to pipe into the spawned command; nil means no stdin.
func (c *Client) PushDriver(sessionID, command string, input []byte) error {
	_, err := sendJSONEvent[RespCreateSession](c, state.EventPushDriver, state.PushDriverParams{
		SessionID: sessionID,
		Command:   command,
		Input:     input,
	})
	return err
}

// PeerSend sends a peer message to the target frame.
func (c *Client) PeerSend(fromFrameID, toFrameID, text, replyTo string) error {
	_, err := c.sendDefault(CmdPeerSend{
		FromFrameID: fromFrameID,
		ToFrameID:   toFrameID,
		Text:        text,
		ReplyTo:     replyTo,
	})
	return err
}

// PeerList lists peers visible to the caller frame.
func (c *Client) PeerList(fromFrameID, scope string) ([]PeerPeerInfo, error) {
	resp, err := c.sendDefault(CmdPeerList{
		FromFrameID: fromFrameID,
		Scope:       scope,
	})
	if err != nil {
		return nil, err
	}
	r, ok := resp.(RespPeerList)
	if !ok {
		return nil, fmt.Errorf("proto: unexpected response for peer.list")
	}
	return r.Peers, nil
}

// PeerSetSummary updates the caller's peer summary.
func (c *Client) PeerSetSummary(fromFrameID, summary string) error {
	_, err := c.sendDefault(CmdPeerSetSummary{
		FromFrameID: fromFrameID,
		Summary:     summary,
	})
	return err
}

// PeerDrainInbox reads and clears the peer inbox for the given frame.
func (c *Client) PeerDrainInbox(frameID string) ([]PeerMessage, error) {
	resp, err := c.sendDefault(CmdPeerDrainInbox{FromFrameID: frameID})
	if err != nil {
		return nil, err
	}
	r, ok := resp.(RespPeerDrainInbox)
	if !ok {
		return nil, fmt.Errorf("proto: unexpected response for peer.drain_inbox")
	}
	return r.Messages, nil
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

// ActivateOccupant changes what occupies the main pane (0.1).
// kind must be "main", "log", or "frame"; session/frame IDs are only
// needed for kind="frame".
func (c *Client) ActivateOccupant(kind, sessionID, frameID string) error {
	_, err := sendJSONEvent[RespOK](c, state.EventActivateOccupant, state.ActivateOccupantParams{
		Kind:      state.OccupantKind(kind),
		SessionID: sessionID,
		FrameID:   frameID,
	})
	return err
}

// ActivateLog respawns pane 0.1 with the log TUI.
func (c *Client) ActivateLog() error { return c.ActivateOccupant("log", "", "") }

// ActivateMain restores pane 0.1 to the main TUI.
func (c *Client) ActivateMain() error { return c.ActivateOccupant("main", "", "") }

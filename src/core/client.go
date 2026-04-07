package core

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
)

type Client struct {
	conn      net.Conn
	encoder   *json.Encoder
	decoder   *json.Decoder
	mu        sync.Mutex
	events    chan Message
	responses chan Message
	done      chan struct{}
}

func Dial(sockPath string) (*Client, error) {
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", sockPath, err)
	}
	c := &Client{
		conn:      conn,
		encoder:   json.NewEncoder(conn),
		decoder:   json.NewDecoder(conn),
		events:    make(chan Message, 64),
		responses: make(chan Message, 8),
		done:      make(chan struct{}),
	}
	return c, nil
}

func (c *Client) Close() {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	c.conn.Close()
}

func (c *Client) Events() <-chan Message {
	return c.events
}

func (c *Client) StartListening() {
	go c.listen()
}

func (c *Client) Subscribe() {
	c.sendCommand("subscribe", nil)
}

func (c *Client) listen() {
	defer close(c.events)
	for {
		var msg Message
		if err := c.decoder.Decode(&msg); err != nil {
			return
		}
		switch msg.Type {
		case "response":
			c.responses <- msg
		case "event":
			c.events <- msg
		}
	}
}

func (c *Client) sendCommand(cmd string, args map[string]string) (Message, error) {
	c.mu.Lock()
	err := c.encoder.Encode(NewCommand(cmd, args))
	c.mu.Unlock()
	if err != nil {
		return Message{}, err
	}
	select {
	case resp := <-c.responses:
		if resp.Error != "" {
			return resp, fmt.Errorf("%s", resp.Error)
		}
		return resp, nil
	case <-c.done:
		return Message{}, fmt.Errorf("client closed")
	}
}

func (c *Client) CreateSession(project, command string) ([]SessionInfo, error) {
	resp, err := c.sendCommand("create-session", map[string]string{
		"project": project,
		"command": command,
	})
	return resp.Sessions, err
}

func (c *Client) StopSession(id string) error {
	_, err := c.sendCommand("stop-session", map[string]string{"session_id": id})
	return err
}

func (c *Client) ListSessions() ([]SessionInfo, string, error) {
	resp, err := c.sendCommand("list-sessions", nil)
	return resp.Sessions, resp.ActiveWindowID, err
}



func (c *Client) Shutdown() error {
	_, err := c.sendCommand("shutdown", nil)
	return err
}

func (c *Client) Detach() error {
	_, err := c.sendCommand("detach", nil)
	return err
}

func (c *Client) PreviewSession(sessionID string) (string, error) {
	resp, err := c.sendCommand("preview-session", map[string]string{
		"session_id": sessionID,
	})
	return resp.ActiveWindowID, err
}

func (c *Client) SwitchSession(sessionID string) (string, error) {
	resp, err := c.sendCommand("switch-session", map[string]string{
		"session_id": sessionID,
	})
	return resp.ActiveWindowID, err
}

func (c *Client) PreviewProject(project string) error {
	_, err := c.sendCommand("preview-project", map[string]string{"project": project})
	return err
}

func (c *Client) FocusPane(pane string) error {
	_, err := c.sendCommand("focus-pane", map[string]string{"pane": pane})
	return err
}

func (c *Client) SendAgentEvent(eventType string, args map[string]string) error {
	a := make(map[string]string, len(args)+1)
	for k, v := range args {
		a[k] = v
	}
	a["type"] = eventType
	_, err := c.sendCommand("agent-event", a)
	return err
}

func (c *Client) LaunchTool(toolName string, args map[string]string) {
	a := make(map[string]string, len(args)+1)
	for k, v := range args {
		a[k] = v
	}
	a["tool"] = toolName
	c.sendCommand("launch-tool", a)
}

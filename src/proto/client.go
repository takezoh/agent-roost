package proto

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// Client is the typed IPC client used by the TUI processes and the
// hook bridge. Each instance owns one Unix socket connection, runs a
// reader goroutine that demuxes responses (matched by req_id) and
// events (pushed onto an events channel), and exposes Send / Events.
type Client struct {
	conn   net.Conn
	writer *bufio.Writer
	writeMu sync.Mutex

	gen *ReqIDGen

	pendingMu sync.Mutex
	pending   map[string]chan inFlight

	events chan ServerEvent

	closeOnce sync.Once
	closed    chan struct{}
	closeErr  error
}

// inFlight is the value the reader goroutine sends back to a Send
// caller waiting on a response. Body holds the unmarshalled response
// (success path); Err holds the parsed error (error path).
type inFlight struct {
	Body Response
	Err  *ErrorBody
}

// Dial opens a Unix socket connection to the daemon and starts the
// reader goroutine.
func Dial(sockPath string) (*Client, error) {
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("proto: dial %s: %w", sockPath, err)
	}
	c := &Client{
		conn:    conn,
		writer:  bufio.NewWriter(conn),
		gen:     NewReqIDGen(),
		pending: map[string]chan inFlight{},
		events:  make(chan ServerEvent, 64),
		closed:  make(chan struct{}),
	}
	go c.read()
	return c, nil
}

// Close shuts down the connection. Idempotent.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
		c.closeErr = c.conn.Close()
		// Wake up any pending Send waiters.
		c.pendingMu.Lock()
		for _, ch := range c.pending {
			close(ch)
		}
		c.pending = nil
		c.pendingMu.Unlock()
	})
	return c.closeErr
}

// Events returns the channel ServerEvent values arrive on. The
// channel is closed when the client disconnects.
func (c *Client) Events() <-chan ServerEvent { return c.events }

// Send writes a typed command and waits for the response. The reply
// is decoded into a typed Response value of the appropriate variant.
// Pass a context with a deadline to bound the wait.
//
// Returns either the decoded Response or an *ErrorBody (the typed
// error body the daemon sent back). The error path uses the standard
// error interface; callers can use errors.As to recover the typed
// body when they care about the code:
//
//	resp, err := client.Send(ctx, proto.CmdStopSession{SessionID: id})
//	var ebody *proto.ErrorBody
//	if errors.As(err, &ebody) && ebody.Code == proto.ErrNotFound { ... }
func (c *Client) Send(ctx context.Context, cmd Command) (Response, error) {
	reqID := c.gen.Next()
	wire, err := EncodeCommand(reqID, cmd)
	if err != nil {
		return nil, err
	}

	ch := make(chan inFlight, 1)
	c.pendingMu.Lock()
	if c.pending == nil {
		c.pendingMu.Unlock()
		return nil, errors.New("proto: client closed")
	}
	c.pending[reqID] = ch
	c.pendingMu.Unlock()

	if err := c.writeFrame(wire); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, reqID)
		c.pendingMu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, reqID)
		c.pendingMu.Unlock()
		return nil, ctx.Err()
	case <-c.closed:
		return nil, errors.New("proto: client closed")
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Body, nil
	}
}

// SendNoWait fires a command and returns immediately, ignoring the
// response. Used by hook bridges that don't care about the reply.
func (c *Client) SendNoWait(cmd Command) error {
	reqID := c.gen.Next()
	wire, err := EncodeCommand(reqID, cmd)
	if err != nil {
		return err
	}
	return c.writeFrame(wire)
}

// SendWithTimeout is the bounded version — waits up to timeout
// for a response, but returns nil on success without parsing the
// body. Used by `roost claude event` so the hook bridge knows the
// daemon accepted the event.
func (c *Client) SendWithTimeout(cmd Command, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, err := c.Send(ctx, cmd)
	return err
}

func (c *Client) writeFrame(payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.writer.Write(payload); err != nil {
		return err
	}
	if err := c.writer.WriteByte('\n'); err != nil {
		return err
	}
	return c.writer.Flush()
}

// read is the reader goroutine. It blocks on the socket, demuxes
// envelopes by Type, and routes them to either pending Send waiters
// (resp) or the events channel (evt).
func (c *Client) read() {
	defer close(c.events)
	dec := json.NewDecoder(c.conn)
	for {
		var env Envelope
		if err := dec.Decode(&env); err != nil {
			return
		}
		c.dispatch(env)
	}
}

func (c *Client) dispatch(env Envelope) {
	switch env.Type {
	case TypeResponse:
		c.dispatchResponse(env)
	case TypeEvent:
		ev, err := DecodeEvent(env)
		if err != nil {
			return
		}
		select {
		case c.events <- ev:
		default:
			// drop on full channel
		}
	}
}

func (c *Client) dispatchResponse(env Envelope) {
	c.pendingMu.Lock()
	ch, ok := c.pending[env.ReqID]
	if ok {
		delete(c.pending, env.ReqID)
	}
	c.pendingMu.Unlock()
	if !ok {
		return
	}

	if env.Status == StatusError {
		errBody := env.Error
		if errBody == nil {
			errBody = &ErrorBody{Code: ErrUnknown, Message: "server returned error without body"}
		}
		ch <- inFlight{Err: errBody}
		return
	}

	body, err := decodeResponseByCommand(env)
	if err != nil {
		ch <- inFlight{Err: &ErrorBody{Code: ErrInternal, Message: err.Error()}}
		return
	}
	ch <- inFlight{Body: body}
}

// decodeResponseByCommand picks the right Response variant for the
// envelope's data. Without the original command name in the response
// envelope, we use a heuristic: try the richest variants first
// (RespCreateSession / RespSessions / RespActiveWindow), fall back
// to RespOK on empty data.
func decodeResponseByCommand(env Envelope) (Response, error) {
	if len(env.Data) == 0 {
		return RespOK{}, nil
	}
	// Try each typed variant in turn. The variants have disjoint
	// JSON shapes (different field names) so the wrong type leaves
	// fields zero-valued — but we want a strict match. Use a peek-
	// based dispatch instead.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(env.Data, &probe); err != nil {
		return RespOK{}, nil
	}
	switch {
	case has(probe, "session_id") && has(probe, "window_id"):
		var r RespCreateSession
		if err := json.Unmarshal(env.Data, &r); err != nil {
			return nil, err
		}
		return r, nil
	case has(probe, "sessions"):
		var r RespSessions
		if err := json.Unmarshal(env.Data, &r); err != nil {
			return nil, err
		}
		return r, nil
	case has(probe, "active_window_id"):
		var r RespActiveWindow
		if err := json.Unmarshal(env.Data, &r); err != nil {
			return nil, err
		}
		return r, nil
	}
	return RespOK{}, nil
}

func has(m map[string]json.RawMessage, key string) bool {
	_, ok := m[key]
	return ok
}

// Error returns the error string for ErrorBody so it satisfies the
// standard error interface, allowing errors.As-style recovery.
func (e *ErrorBody) Error() string {
	if e == nil {
		return "<nil>"
	}
	return string(e.Code) + ": " + e.Message
}

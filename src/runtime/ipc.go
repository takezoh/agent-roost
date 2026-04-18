package runtime

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"

	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/state"
)

// ipcConn is one accepted client connection. The reader goroutine
// decodes envelopes off the socket and forwards typed Commands to
// the runtime as state.Event values; the writer goroutine drains
// outbox and writes wire bytes to the socket.
type ipcConn struct {
	id      state.ConnID
	conn    net.Conn
	outbox  chan []byte
	done    chan struct{}
	once    sync.Once
	writeMu sync.Mutex
}

const ipcOutboxSize = 64

func newIPCConn(id state.ConnID, conn net.Conn) *ipcConn {
	return &ipcConn{
		id:     id,
		conn:   conn,
		outbox: make(chan []byte, ipcOutboxSize),
		done:   make(chan struct{}),
	}
}

// shut closes the connection and signals the writer to exit.
// Idempotent.
func (cc *ipcConn) shut() {
	cc.once.Do(func() {
		close(cc.done)
		cc.conn.Close()
	})
}

// === Listener / accept loop ===

// StartIPC opens the Unix socket and starts the accept loop. Should
// be called from main after Run is already running (so the accept
// loop can call Enqueue).
func (r *Runtime) StartIPC(sockPath string) error {
	os.Remove(sockPath)
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return fmt.Errorf("runtime: listen %s: %w", sockPath, err)
	}
	// Restrict socket to owner only — roost controls tmux session
	// lifecycle, so unauthenticated local access = arbitrary command
	// execution.
	if err := os.Chmod(sockPath, 0o600); err != nil {
		ln.Close()
		return fmt.Errorf("runtime: chmod %s: %w", sockPath, err)
	}
	r.listener = ln
	slog.Info("runtime: ipc listening", "sock", sockPath)
	go r.acceptLoop()
	return nil
}

func (r *Runtime) acceptLoop() {
	for {
		conn, err := r.listener.Accept()
		if err != nil {
			select {
			case <-r.done:
				return
			default:
				if errors.Is(err, net.ErrClosed) {
					return
				}
				slog.Error("runtime: accept failed", "err", err)
				continue
			}
		}
		r.enqueueInternal(connOpen{conn: conn})
	}
}

// internalEvent is the closed sum of runtime-internal lifecycle
// events that bypass state.Reduce. Used for connection accept /
// teardown, where the runtime owns mutable state (the conns map)
// the reducer can't see.
type internalEvent interface {
	isInternalEvent()
}

// connOpen is enqueued by the accept loop after Accept returns.
type connOpen struct {
	conn net.Conn
}

func (connOpen) isInternalEvent() {}

// connClose is enqueued by connReader after the socket EOFs.
type connClose struct {
	id state.ConnID
}

func (connClose) isInternalEvent() {}

// internalSetRelay is enqueued by SetRelay to wire a FileRelay into the loop.
type internalSetRelay struct {
	relay *FileRelay
}

func (internalSetRelay) isInternalEvent() {}

// dispatchInternal handles runtime-internal events.
func (r *Runtime) dispatchInternal(ev internalEvent) {
	switch e := ev.(type) {
	case connOpen:
		r.handleConnOpen(e.conn)
	case connClose:
		r.handleConnClose(e.id)
	case internalSetRelay:
		r.relay = e.relay
		r.syncRelayWatches()
	}
}

// enqueueInternal posts an internal event onto the runtime's
// internal channel. Non-blocking; drops with a warning on a full
// channel.
func (r *Runtime) enqueueInternal(ev internalEvent) {
	select {
	case r.internalCh <- ev:
	default:
		slog.Warn("runtime: internal channel full, dropping")
	}
}

func (r *Runtime) handleConnOpen(conn net.Conn) {
	r.nextConn++
	id := r.nextConn
	cc := newIPCConn(id, conn)
	r.conns[id] = cc
	go r.connWriter(cc)
	go r.connReader(cc)
	r.dispatch(state.EvConnOpened{ConnID: id})
}

// connReader decodes wire envelopes, translates Commands into
// state.Events, and enqueues them on the runtime event loop. On EOF
// or error, it enqueues EvConnClosed and exits.
func (r *Runtime) connReader(cc *ipcConn) {
	defer func() {
		r.enqueueInternal(connClose{id: cc.id})
	}()
	dec := json.NewDecoder(cc.conn)
	for {
		select {
		case <-cc.done:
			return
		default:
		}
		var env proto.Envelope
		if err := dec.Decode(&env); err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				slog.Debug("runtime: conn decode error", "conn", cc.id, "err", err)
			}
			return
		}
		if env.Type != proto.TypeCommand {
			continue
		}
		cmd, err := proto.DecodeCommand(env)
		if err != nil {
			slog.Warn("runtime: bad command", "conn", cc.id, "err", err)
			r.sendErrorImmediate(cc, env.ReqID, proto.ErrInvalidArgument, err.Error())
			continue
		}
		ev := commandToStateEvent(cc.id, env.ReqID, cmd)
		if ev == nil {
			r.sendErrorImmediate(cc, env.ReqID, proto.ErrUnsupported, "unknown command")
			continue
		}
		r.Enqueue(ev)
	}
}

// connWriter drains the outbox and writes wire bytes to the socket.
func (r *Runtime) connWriter(cc *ipcConn) {
	for {
		select {
		case <-cc.done:
			return
		case payload := <-cc.outbox:
			if err := r.writeWire(cc, payload); err != nil {
				return
			}
		}
	}
}

func (r *Runtime) handleConnClose(id state.ConnID) {
	if cc, ok := r.conns[id]; ok {
		cc.shut()
		delete(r.conns, id)
	}
	r.dispatch(state.EvConnClosed{ConnID: id})
}

// sendErrorImmediate writes an error response on a connection
// without going through the reducer (used for malformed envelopes
// caught in connReader, before the event loop ever sees them).
func (r *Runtime) sendErrorImmediate(cc *ipcConn, reqID string, code proto.ErrCode, msg string) {
	wire, err := proto.EncodeError(reqID, code, msg, nil)
	if err != nil {
		return
	}
	r.queueWire(cc, wire)
}

// queueWire enqueues raw wire bytes on a conn's outbox. Non-blocking;
// drops with a warning if the outbox is full.
func (r *Runtime) queueWire(cc *ipcConn, wire []byte) {
	select {
	case cc.outbox <- wire:
	case <-cc.done:
	default:
		slog.Warn("runtime: conn outbox full, dropping", "conn", cc.id)
	}
}

func (r *Runtime) writeWire(cc *ipcConn, wire []byte) error {
	cc.writeMu.Lock()
	defer cc.writeMu.Unlock()

	select {
	case <-cc.done:
		return net.ErrClosed
	default:
	}

	w := bufio.NewWriter(cc.conn)
	if _, err := w.Write(wire); err != nil {
		return err
	}
	if err := w.WriteByte('\n'); err != nil {
		return err
	}
	return w.Flush()
}

// shutdownIPC closes the listener and every active connection. Called
// from Run on shutdown.
func (r *Runtime) shutdownIPC() {
	if r.listener != nil {
		r.listener.Close()
	}
	for id, cc := range r.conns {
		cc.shut()
		delete(r.conns, id)
	}
}

// === Loop integration ===

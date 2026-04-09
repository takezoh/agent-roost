package core

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/take/agent-roost/session/driver"
	"github.com/take/agent-roost/tmux"
)

// Server is the IPC entry point for roost. It owns the unix socket, the
// per-connection clients list, and the broadcast pipeline.
//
// Server is implemented as an actor: a single goroutine (started by
// run()) owns `clients` and processes mutations through `inbox`.
// handleConn goroutines submit closures via exec() — they never touch
// `clients` directly. Each clientConn has its own writer goroutine that
// drains a buffered outbox channel, so a slow client cannot block the
// server actor or any other client.
type Server struct {
	coord    *Coordinator
	tmux     *tmux.Client
	listener net.Listener
	sockPath string
	aliases  map[string]string

	// actor primitives
	inbox     chan func()
	stop      chan struct{}
	stopped   chan struct{}
	closeOnce sync.Once

	// owned by the server actor goroutine — never touched from outside
	clients []*clientConn

	// shutdownRequested is set inside the actor (handleShutdown) and read
	// from main() after Attach() returns; an atomic.Bool keeps it lock-free.
	shutdownRequested atomic.Bool
}

// clientConn represents one accepted IPC connection. The writer goroutine
// drains `outbox` and writes JSON-encoded messages to the socket. The
// actor signals shutdown by closing `done`; the writer notices this on
// the next select cycle and exits.
//
// `broadcastEnabled` is mutated only from the server actor goroutine
// inside the subscribe handler, so no synchronization is needed there.
// `outbox` is a buffered channel (size 64) — sends from any goroutine
// are safe; if it fills up, the message is dropped and a warning is
// logged. Realistic outbox depth is 1-2 messages, so dropping only
// happens for misbehaving clients.
type clientConn struct {
	conn             net.Conn
	outbox           chan Message
	done             chan struct{}
	closeOnce        sync.Once
	broadcastEnabled bool
}

const clientOutboxSize = 64

func newClientConn(conn net.Conn) *clientConn {
	return &clientConn{
		conn:   conn,
		outbox: make(chan Message, clientOutboxSize),
		done:   make(chan struct{}),
	}
}

// shut releases the connection and signals the writer to exit. Idempotent.
func (cc *clientConn) shut() {
	cc.closeOnce.Do(func() {
		close(cc.done)
		cc.conn.Close()
	})
}

func NewServer(coord *Coordinator, tmuxClient *tmux.Client, sockPath string) *Server {
	return &Server{
		coord:    coord,
		tmux:     tmuxClient,
		sockPath: sockPath,
		inbox:    make(chan func(), 32),
		stop:     make(chan struct{}),
		stopped:  make(chan struct{}),
	}
}

func (s *Server) Done() <-chan struct{} { return s.stopped }

func (s *Server) ShutdownRequested() bool { return s.shutdownRequested.Load() }

func (s *Server) Start() error {
	os.Remove(s.sockPath)
	ln, err := net.Listen("unix", s.sockPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.sockPath, err)
	}
	s.listener = ln
	slog.Info("server listening", "sock", s.sockPath)
	go s.run()
	go s.acceptLoop()
	return nil
}

// run is the server actor goroutine. It processes inbox closures one at
// a time and tears down all clients on shutdown.
func (s *Server) run() {
	defer close(s.stopped)
	for {
		select {
		case <-s.stop:
			for _, cc := range s.clients {
				cc.shut()
			}
			s.clients = nil
			return
		case fn := <-s.inbox:
			fn()
		}
	}
}

// exec submits fn to the server actor and waits for it to complete.
// If the actor has already shut down, fn is dropped silently.
func (s *Server) exec(fn func()) {
	done := make(chan struct{})
	select {
	case <-s.stop:
		return
	case s.inbox <- func() { fn(); close(done) }:
	}
	select {
	case <-done:
	case <-s.stopped:
	}
}

func (s *Server) Stop() {
	s.closeOnce.Do(func() {
		slog.Info("server stopping")
		close(s.stop)
		if s.listener != nil {
			s.listener.Close()
		}
		<-s.stopped
		os.Remove(s.sockPath)
	})
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.stop:
				return
			default:
				slog.Error("accept failed", "err", err)
				continue
			}
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	slog.Debug("client connected")
	cc := newClientConn(conn)
	s.exec(func() { s.clients = append(s.clients, cc) })
	go s.writeLoop(cc)
	defer func() {
		if cc.broadcastEnabled {
			slog.Info("subscriber disconnected")
		} else {
			slog.Debug("client disconnected")
		}
		s.exec(func() {
			for i, c := range s.clients {
				if c == cc {
					s.clients = append(s.clients[:i], s.clients[i+1:]...)
					break
				}
			}
		})
		cc.shut()
	}()
	dec := json.NewDecoder(conn)
	for {
		var msg Message
		if err := dec.Decode(&msg); err != nil {
			return
		}
		if msg.Type != "command" {
			continue
		}
		s.dispatch(cc, msg)
	}
}

// writeLoop drains the client's outbox and writes JSON-encoded messages
// to the underlying socket. Exits when `done` is closed (server shutdown
// or client removal) or when the encoder reports an I/O error.
func (s *Server) writeLoop(cc *clientConn) {
	encoder := json.NewEncoder(cc.conn)
	for {
		select {
		case <-cc.done:
			return
		case msg := <-cc.outbox:
			if err := encoder.Encode(msg); err != nil {
				return
			}
		}
	}
}

// sendTo enqueues a message on the client's outbox. Non-blocking — if
// the outbox is full or the client is shutting down, the message is
// dropped with a warning. Safe to call from any goroutine.
func (s *Server) sendTo(cc *clientConn, msg Message) {
	select {
	case <-cc.done:
	case cc.outbox <- msg:
	default:
		slog.Warn("client outbox full, dropping message", "type", msg.Type, "event", msg.Event)
	}
}

func (s *Server) dispatch(cc *clientConn, msg Message) {
	if msg.Command == "agent-event" {
		slog.Debug("dispatch", "command", msg.Command)
	} else {
		slog.Info("dispatch", "command", msg.Command)
	}
	switch msg.Command {
	case "subscribe":
		s.exec(func() { cc.broadcastEnabled = true })
		slog.Info("subscriber connected")
		s.sendResponse(cc, Message{})
		s.sendTo(cc, s.buildSessionsEvent(false))
	case "create-session":
		s.handleCreateSession(cc, msg.Args)
	case "stop-session":
		s.handleStopSession(cc, msg.Args)
	case "list-sessions":
		s.handleListSessions(cc)
	case "shutdown":
		s.handleShutdown(cc)
	case "preview-session":
		s.handlePreviewSession(cc, msg.Args)
	case "switch-session":
		s.handleSwitchSession(cc, msg.Args)
	case "focus-pane":
		s.handleFocusPane(cc, msg.Args)
	case "preview-project":
		s.handlePreviewProject(cc, msg.Args)
	case "launch-tool":
		s.handleLaunchTool(cc, msg.Args)
	case "agent-event":
		s.handleAgentEvent(cc, msg.Args)
	case "detach":
		s.handleDetach(cc)
	default:
		s.sendError(cc, "unknown command: "+msg.Command)
	}
}

func (s *Server) handleCreateSession(cc *clientConn, args map[string]string) {
	project := args["project"]
	command := args["command"]
	if project == "" {
		s.sendError(cc, "missing project arg")
		return
	}
	if command == "" {
		command = "claude"
	}
	if expanded := ResolveCommandAlias(s.aliases, command); expanded != command {
		slog.Info("alias expanded", "from", command, "to", expanded)
		command = expanded
	}
	slog.Info("create session", "project", project, "command", command)
	id, err := s.coord.Create(project, command)
	if err != nil {
		slog.Error("create session failed", "err", err)
		s.sendError(cc, err.Error())
		return
	}
	s.coord.Switch(id)
	infos, active := s.coord.SnapshotSessionsAndActive()
	s.sendResponse(cc, Message{
		Sessions:       infos,
		ActiveWindowID: active,
	})
	s.broadcastSessions()
}

func (s *Server) handleStopSession(cc *clientConn, args map[string]string) {
	id := args["session_id"]
	slog.Info("stop session", "id", id)
	if id == "" {
		s.sendError(cc, "missing id arg")
		return
	}
	if err := s.coord.Stop(id); err != nil {
		s.sendError(cc, err.Error())
		return
	}
	s.sendResponse(cc, Message{})
	s.broadcastSessions()
}

func (s *Server) handleListSessions(cc *clientConn) {
	infos, active := s.coord.SnapshotSessionsAndActive()
	s.sendResponse(cc, Message{
		Sessions:       infos,
		ActiveWindowID: active,
	})
}

func (s *Server) handleShutdown(cc *clientConn) {
	slog.Info("shutdown requested")
	s.shutdownRequested.Store(true)
	s.sendResponse(cc, Message{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		s.tmux.DetachClient()
	}()
}

func (s *Server) handlePreviewSession(cc *clientConn, args map[string]string) {
	id := args["session_id"]
	slog.Info("preview session", "id", id)
	if err := s.coord.Preview(id); err != nil {
		s.sendError(cc, err.Error())
		return
	}
	s.coord.SyncActiveStatusLine()
	s.broadcastPreview()
	s.sendResponse(cc, Message{
		ActiveWindowID: s.coord.ActiveWindowID(),
	})
}

func (s *Server) handleSwitchSession(cc *clientConn, args map[string]string) {
	id := args["session_id"]
	slog.Info("switch session", "id", id)
	if err := s.coord.Switch(id); err != nil {
		s.sendError(cc, err.Error())
		return
	}
	s.coord.SyncActiveStatusLine()
	s.broadcastSessions()
	s.sendResponse(cc, Message{
		ActiveWindowID: s.coord.ActiveWindowID(),
	})
}

func (s *Server) handlePreviewProject(cc *clientConn, args map[string]string) {
	slog.Info("preview project", "project", args["project"])
	if err := s.coord.Deactivate(); err != nil {
		s.sendResponse(cc, Message{})
		return
	}
	s.sendResponse(cc, Message{})
	msg := NewEvent("project-selected")
	msg.SelectedProject = args["project"]
	s.broadcast(msg)
}

func (s *Server) handleFocusPane(cc *clientConn, args map[string]string) {
	s.coord.FocusPane(args["pane"])
	s.sendResponse(cc, Message{})
}

func (s *Server) handleLaunchTool(cc *clientConn, args map[string]string) {
	toolName := args["tool"]
	toolArgs := make(map[string]string, len(args)-1)
	for k, v := range args {
		if k != "tool" {
			toolArgs[k] = v
		}
	}
	s.coord.LaunchTool(toolName, toolArgs)
	s.sendResponse(cc, Message{})
}

func (s *Server) handleDetach(cc *clientConn) {
	slog.Info("detach requested")
	if err := s.tmux.DetachClient(); err != nil {
		s.sendError(cc, err.Error())
		return
	}
	s.sendResponse(cc, Message{})
}

func (s *Server) handleAgentEvent(cc *clientConn, args map[string]string) {
	// AgentEventFromArgs is the single string-key boundary in core: every
	// downstream call uses struct fields so the rest of the coordinator
	// never has to know which agent driver produced the event. The driver
	// is responsible for any side-effects (event log, etc.) — server only
	// routes the event and rebroadcasts.
	ev := driver.AgentEventFromArgs(args)
	switch ev.Type {
	case driver.AgentEventSessionStart:
		_, consumed := s.coord.HandleHookEvent(ev)
		if consumed {
			s.broadcastSessions()
		}
		s.sendResponse(cc, Message{})
	case driver.AgentEventStateChange:
		if ev.State == "" {
			s.sendError(cc, "state-change: state required")
			return
		}
		_, consumed := s.coord.HandleHookEvent(ev)
		if !consumed {
			s.sendError(cc, "state-change: rejected by driver (state="+ev.State+")")
			return
		}
		s.broadcastSessions()
		s.sendResponse(cc, Message{})
	default:
		s.sendError(cc, "unknown agent event type: "+string(ev.Type))
	}
}

func (s *Server) sendResponse(cc *clientConn, msg Message) {
	msg.Type = "response"
	s.sendTo(cc, msg)
}

func (s *Server) sendError(cc *clientConn, errMsg string) {
	s.sendResponse(cc, Message{Error: errMsg})
}

// broadcast snapshots the current clients list inside the actor and then
// queues the message on each subscriber's outbox. The actor goroutine
// touches `clients` only — it never blocks on I/O because each clientConn
// has its own writer goroutine.
func (s *Server) broadcast(msg Message) {
	s.exec(func() {
		for _, cc := range s.clients {
			if cc.broadcastEnabled {
				s.sendTo(cc, msg)
			}
		}
	})
}

func (s *Server) broadcastSessions() { s.broadcast(s.buildSessionsEvent(false)) }
func (s *Server) broadcastPreview()  { s.broadcast(s.buildSessionsEvent(true)) }

func (s *Server) buildSessionsEvent(preview bool) Message {
	infos, active := s.coord.SnapshotSessionsAndActive()
	msg := NewEvent("sessions-changed")
	msg.Sessions = infos
	msg.ActiveWindowID = active
	msg.IsPreview = preview
	return msg
}

// AsyncBroadcast queues `msg` for broadcast without waiting for the
// server actor to process it. Used by Coordinator's notification
// callback to fire sessions-changed events from inside its own actor
// goroutine without forming a deadlock cycle (Server actor never calls
// back into Coordinator, so this one-way edge is safe).
//
// If the inbox is full the message is dropped with a warning — the next
// tick will produce a fresh sessions-changed payload anyway.
func (s *Server) AsyncBroadcast(msg Message) {
	closure := func() {
		for _, cc := range s.clients {
			if cc.broadcastEnabled {
				s.sendTo(cc, msg)
			}
		}
	}
	select {
	case <-s.stop:
	case s.inbox <- closure:
	default:
		slog.Warn("server inbox full, dropping coordinator broadcast", "event", msg.Event)
	}
}

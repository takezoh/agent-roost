package core

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/tmux"
)

type Server struct {
	svc               *Service
	tmux              *tmux.Client
	listener          net.Listener
	clients           []*clientConn
	mu                sync.Mutex
	sockPath          string
	done              chan struct{}
	shutdownRequested bool
}

type clientConn struct {
	conn       net.Conn
	encoder    *json.Encoder
	broadcastEnabled bool
}

func NewServer(svc *Service, tmuxClient *tmux.Client, sockPath string) *Server {
	return &Server{
		svc:      svc,
		tmux:     tmuxClient,
		sockPath: sockPath,
		done:     make(chan struct{}),
	}
}

func (s *Server) Done() <-chan struct{} {
	return s.done
}

func (s *Server) ShutdownRequested() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shutdownRequested
}

func (s *Server) Start() error {
	os.Remove(s.sockPath)

	ln, err := net.Listen("unix", s.sockPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.sockPath, err)
	}
	s.listener = ln

	go s.acceptLoop()
	return nil
}

func (s *Server) Stop() {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	if s.listener != nil {
		s.listener.Close()
	}
	s.mu.Lock()
	for _, c := range s.clients {
		c.conn.Close()
	}
	s.clients = nil
	s.mu.Unlock()
	os.Remove(s.sockPath)
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				log.Printf("accept: %v", err)
				continue
			}
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	cc := &clientConn{conn: conn, encoder: json.NewEncoder(conn)}
	s.addClient(cc)
	defer s.removeClient(cc)

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

func (s *Server) dispatch(cc *clientConn, msg Message) {
	switch msg.Command {
	case "subscribe":
		cc.broadcastEnabled = true
		s.sendResponse(cc, Message{})
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
	case "launch-tool":
		s.handleLaunchTool(cc, msg.Args)
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
	sess, err := s.svc.Manager.Create(project, command)
	if err != nil {
		s.sendError(cc, err.Error())
		return
	}
	activeID := args["active_window_id"]
	if activeID != "" {
		s.svc.Switch(sess, activeID)
	}
	s.sendResponse(cc, Message{Sessions: SessionsToInfo(s.svc.Sessions())})
	s.broadcastSessions()
}

func (s *Server) handleStopSession(cc *clientConn, args map[string]string) {
	id := args["session_id"]
	if id == "" {
		s.sendError(cc, "missing id arg")
		return
	}
	if err := s.svc.Manager.Stop(id); err != nil {
		s.sendError(cc, err.Error())
		return
	}
	s.sendResponse(cc, Message{})
	s.broadcastSessions()
}

func (s *Server) handleListSessions(cc *clientConn) {
	s.sendResponse(cc, Message{Sessions: SessionsToInfo(s.svc.Sessions())})
}

func (s *Server) handleShutdown(cc *clientConn) {
	s.mu.Lock()
	s.shutdownRequested = true
	s.mu.Unlock()

	s.sendResponse(cc, Message{})

	go func() {
		time.Sleep(50 * time.Millisecond)
		s.tmux.DetachClient()
	}()
}

func (s *Server) handlePreviewSession(cc *clientConn, args map[string]string) {
	sess := s.findSession(args["session_id"])
	if sess == nil {
		s.sendError(cc, "session not found: "+args["session_id"])
		return
	}
	if err := s.svc.Preview(sess, args["active_window_id"]); err != nil {
		s.sendError(cc, err.Error())
		return
	}
	s.sendResponse(cc, Message{})
}

func (s *Server) handleSwitchSession(cc *clientConn, args map[string]string) {
	sess := s.findSession(args["session_id"])
	if sess == nil {
		s.sendError(cc, "session not found: "+args["session_id"])
		return
	}
	if err := s.svc.Switch(sess, args["active_window_id"]); err != nil {
		s.sendError(cc, err.Error())
		return
	}
	s.sendResponse(cc, Message{})
}

func (s *Server) handleFocusPane(cc *clientConn, args map[string]string) {
	s.svc.FocusPane(args["pane"])
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
	s.svc.LaunchTool(toolName, toolArgs)
	s.sendResponse(cc, Message{})
}

func (s *Server) findSession(id string) *session.Session {
	for _, sess := range s.svc.Sessions() {
		if sess.ID == id {
			return sess
		}
	}
	return nil
}

func (s *Server) handleDetach(cc *clientConn) {
	if err := s.tmux.DetachClient(); err != nil {
		s.sendError(cc, err.Error())
		return
	}
	s.sendResponse(cc, Message{})
}

func (s *Server) sendResponse(cc *clientConn, msg Message) {
	msg.Type = "response"
	cc.encoder.Encode(msg)
}

func (s *Server) sendError(cc *clientConn, errMsg string) {
	s.sendResponse(cc, Message{Error: errMsg})
}

func (s *Server) broadcast(msg Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cc := range s.clients {
		if cc.broadcastEnabled {
			cc.encoder.Encode(msg)
		}
	}
}

func (s *Server) broadcastSessions() {
	msg := NewEvent("sessions-changed")
	msg.Sessions = SessionsToInfo(s.svc.Sessions())
	s.broadcast(msg)
}

func (s *Server) StartMonitor(intervalMs int) {
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			ids := windowIDs(s.svc.Sessions())
			if len(ids) == 0 {
				continue
			}
			states := s.svc.PollStates(ids)
			s.svc.UpdateStates(states)
			msg := NewEvent("states-updated")
			msg.States = states
			s.broadcast(msg)
		}
	}
}

func (s *Server) addClient(cc *clientConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients = append(s.clients, cc)
}

func (s *Server) removeClient(cc *clientConn) {
	cc.conn.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.clients {
		if c == cc {
			s.clients = append(s.clients[:i], s.clients[i+1:]...)
			return
		}
	}
}

func windowIDs(sessions []*session.Session) []string {
	ids := make([]string, len(sessions))
	for i, s := range sessions {
		ids[i] = s.WindowID
	}
	return ids
}

package core

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
	"github.com/take/agent-roost/tmux"
)

type Server struct {
	svc               *Service
	tmux              *tmux.Client
	drivers           *driver.Registry
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

func NewServer(svc *Service, tmuxClient *tmux.Client, sockPath string, drivers *driver.Registry) *Server {
	return &Server{
		svc:      svc,
		tmux:     tmuxClient,
		drivers:  drivers,
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
	slog.Info("server listening", "sock", s.sockPath)

	go s.acceptLoop()
	return nil
}

func (s *Server) Stop() {
	slog.Info("server stopping", "clients", len(s.clients))
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
				slog.Error("accept failed", "err", err)
				continue
			}
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	slog.Info("client connected")
	cc := &clientConn{conn: conn, encoder: json.NewEncoder(conn)}
	s.addClient(cc)
	defer slog.Info("client disconnected")
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
	slog.Info("dispatch", "command", msg.Command)
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
	case "preview-project":
		s.handlePreviewProject(cc, msg.Args)
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
	slog.Info("create session", "project", project, "command", command)
	sess, err := s.svc.Manager.Create(project, command)
	if err != nil {
		slog.Error("create session failed", "err", err)
		s.sendError(cc, err.Error())
		return
	}
	s.svc.Switch(sess)
	s.sendResponse(cc, Message{
		Sessions:       SessionsToInfo(s.svc.Sessions()),
		ActiveWindowID: s.svc.ActiveWindowID(),
		SessionLogPath: s.svc.ActiveSessionLogPath(),
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
	sess := s.findSession(id)
	var windowID string
	if sess != nil {
		windowID = sess.WindowID
	}
	if err := s.svc.Manager.Stop(id); err != nil {
		s.sendError(cc, err.Error())
		return
	}
	if windowID != "" {
		s.svc.ClearActive(windowID)
	}
	s.sendResponse(cc, Message{})
	s.broadcastSessions()
}

func (s *Server) handleListSessions(cc *clientConn) {
	s.sendResponse(cc, Message{
		Sessions:       SessionsToInfo(s.svc.Sessions()),
		ActiveWindowID: s.svc.ActiveWindowID(),
		SessionLogPath: s.svc.ActiveSessionLogPath(),
	})
}

func (s *Server) handleShutdown(cc *clientConn) {
	slog.Info("shutdown requested")
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
	slog.Info("preview session", "id", args["session_id"])
	sess := s.findSession(args["session_id"])
	if sess == nil {
		s.sendError(cc, "session not found: "+args["session_id"])
		return
	}
	if err := s.svc.Preview(sess); err != nil {
		s.sendResponse(cc, Message{})
		return
	}
	s.sendResponse(cc, Message{
		ActiveWindowID: s.svc.ActiveWindowID(),
		SessionLogPath: s.svc.ActiveSessionLogPath(),
	})
}

func (s *Server) handleSwitchSession(cc *clientConn, args map[string]string) {
	slog.Info("switch session", "id", args["session_id"])
	sess := s.findSession(args["session_id"])
	if sess == nil {
		s.sendError(cc, "session not found: "+args["session_id"])
		return
	}
	if err := s.svc.Switch(sess); err != nil {
		s.sendResponse(cc, Message{})
		return
	}
	s.sendResponse(cc, Message{
		ActiveWindowID: s.svc.ActiveWindowID(),
		SessionLogPath: s.svc.ActiveSessionLogPath(),
	})
}

func (s *Server) handlePreviewProject(cc *clientConn, args map[string]string) {
	slog.Info("preview project", "project", args["project"])
	if err := s.svc.Deactivate(); err != nil {
		s.sendResponse(cc, Message{})
		return
	}
	s.sendResponse(cc, Message{})
	msg := NewEvent("project-selected")
	msg.SelectedProject = args["project"]
	s.broadcast(msg)
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
	slog.Info("detach requested")
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
	msg.ActiveWindowID = s.svc.ActiveWindowID()
	msg.SessionLogPath = s.svc.ActiveSessionLogPath()
	s.broadcast(msg)
}

func (s *Server) StartMonitor(intervalMs int) {
	slog.Info("monitor started", "interval_ms", intervalMs)
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	defer ticker.Stop()
	var titleTick int
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			sessions := s.svc.Sessions()
			if len(sessions) == 0 {
				continue
			}
			states := s.svc.PollStates(sessions)
			s.svc.UpdateStates(states)
			msg := NewEvent("states-updated")
			msg.States = states
			s.broadcast(msg)

			titleTick++
			if titleTick >= 5 {
				titleTick = 0
				home, _ := os.UserHomeDir()
				fsys := os.DirFS(home)
				metas := make(map[string]session.SessionMeta, len(sessions))
				for _, sess := range sessions {
					m := s.drivers.Get(sess.Command).ResolveMeta(fsys, sess.Project)
					if m.Title != "" || m.LastPrompt != "" {
						metas[sess.ID] = session.SessionMeta{Title: m.Title, LastPrompt: m.LastPrompt}
					}
				}
				if s.svc.Manager.UpdateMeta(metas) {
					s.broadcastSessions()
				}
			}
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

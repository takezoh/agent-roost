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
	listener          net.Listener
	clients           []*clientConn
	mu                sync.Mutex
	sockPath          string
	done              chan struct{}
	shutdownRequested bool
	aliases           map[string]string
}

type clientConn struct {
	conn       net.Conn
	encoder    *json.Encoder
	broadcastEnabled bool
}

func NewServer(svc *Service, tmuxClient *tmux.Client, sockPath string) *Server {
	s := &Server{
		svc:      svc,
		tmux:     tmuxClient,
		sockPath: sockPath,
		done:     make(chan struct{}),
	}
	svc.OnPreview(func(sessionID string) {
		if svc.Manager.RefreshBranch(sessionID) {
			s.broadcastSessions()
		}
	})
	return s
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
	slog.Debug("client connected")
	cc := &clientConn{conn: conn, encoder: json.NewEncoder(conn)}
	s.addClient(cc)
	defer func() {
		if cc.broadcastEnabled {
			slog.Info("subscriber disconnected")
		} else {
			slog.Debug("client disconnected")
		}
		s.removeClient(cc)
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

func (s *Server) dispatch(cc *clientConn, msg Message) {
	if msg.Command == "agent-event" {
		slog.Debug("dispatch", "command", msg.Command)
	} else {
		slog.Info("dispatch", "command", msg.Command)
	}
	switch msg.Command {
	case "subscribe":
		cc.broadcastEnabled = true
		slog.Info("subscriber connected")
		s.sendResponse(cc, Message{})
		// Push current state immediately
		msg := NewEvent("sessions-changed")
		msg.Sessions = BuildSessionInfos(s.svc.Sessions(), s.svc.AgentStore, s.svc.States)
		msg.ActiveWindowID = s.svc.ActiveWindowID()
		msg.SessionLogPath = s.svc.ActiveSessionLogPath()
		msg.EventLogPath = s.svc.EventLogPathByWindow(s.svc.ActiveWindowID())
		msg.TranscriptPath = s.svc.ActiveTranscriptPath()
		cc.encoder.Encode(msg)
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
	sess, err := s.svc.CreateSession(project, command)
	if err != nil {
		slog.Error("create session failed", "err", err)
		s.sendError(cc, err.Error())
		return
	}
	s.svc.Switch(sess)
	s.sendResponse(cc, Message{
		Sessions:       BuildSessionInfos(s.svc.Sessions(), s.svc.AgentStore, s.svc.States),
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
	if err := s.svc.StopSession(id); err != nil {
		s.sendError(cc, err.Error())
		return
	}
	s.sendResponse(cc, Message{})
	s.broadcastSessions()
}

func (s *Server) handleListSessions(cc *clientConn) {
	s.sendResponse(cc, Message{
		Sessions:       BuildSessionInfos(s.svc.Sessions(), s.svc.AgentStore, s.svc.States),
		ActiveWindowID: s.svc.ActiveWindowID(),
		SessionLogPath: s.svc.ActiveSessionLogPath(),
		EventLogPath:   s.svc.EventLogPathByWindow(s.svc.ActiveWindowID()),
		TranscriptPath: s.svc.ActiveTranscriptPath(),
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
	s.svc.SyncActiveStatusLine()
	s.broadcastSessions()
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
	s.svc.SyncActiveStatusLine()
	s.broadcastSessions()
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

func (s *Server) handleAgentEvent(cc *clientConn, args map[string]string) {
	// AgentEventFromArgs is the single string-key boundary in core: every
	// downstream call uses struct fields so the rest of the coordinator never
	// has to know which agent driver produced the event.
	ev := driver.AgentEventFromArgs(args)
	switch ev.Type {
	case driver.AgentEventSessionStart:
		res := s.svc.ApplyAgentEvent(ev)
		if res.BindChanged || res.StateChanged {
			s.broadcastSessions()
		}
		s.svc.AppendEventLog(res.Identity, "SessionStart")
		s.sendResponse(cc, Message{})
	case driver.AgentEventStateChange:
		if ev.State == "" {
			s.sendError(cc, "state-change: state required")
			return
		}
		// Apply the driver state bag first so the binding exists before the
		// transcript reader needs to look up workdir / transcript_path.
		res := s.svc.ApplyAgentEvent(ev)
		// Route the state-change to the per-window driver Observer. The
		// Observer is the only writer to state.Store for its session — core
		// has no special knowledge of which states a driver supports.
		windowID := s.svc.ResolveWindowID(ev.Pane)
		consumed := false
		if windowID != "" {
			consumed = s.svc.Observers.Dispatch(windowID, ev)
		}
		if !consumed {
			s.sendError(cc, "state-change: rejected by driver observer (state="+ev.State+")")
			return
		}
		usageChanged := false
		if res.Identity != "" {
			usageChanged = s.svc.UpdateStatusFromTranscript(res.Identity)
		}
		if consumed || res.BindChanged || res.StateChanged || usageChanged {
			s.broadcastSessions()
		}
		logLine := ev.Log
		if logLine == "" {
			logLine = ev.State
		}
		s.svc.AppendEventLog(res.Identity, logLine)
		s.sendResponse(cc, Message{})
	default:
		s.sendError(cc, "unknown agent event type: "+string(ev.Type))
	}
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
	msg.Sessions = BuildSessionInfos(s.svc.Sessions(), s.svc.AgentStore, s.svc.States)
	msg.ActiveWindowID = s.svc.ActiveWindowID()
	msg.SessionLogPath = s.svc.ActiveSessionLogPath()
	msg.EventLogPath = s.svc.EventLogPathByWindow(s.svc.ActiveWindowID())
	msg.TranscriptPath = s.svc.ActiveTranscriptPath()
	s.broadcast(msg)
}


func (s *Server) StartMonitor(intervalMs int) {
	slog.Info("monitor started", "interval_ms", intervalMs)
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	defer ticker.Stop()
	const metaResolveCycle = 5
	titleTick := metaResolveCycle - 1 // resolve on first tick
	for {
		select {
		case <-s.done:
			return
		case t := <-ticker.C:
			if reaped := s.svc.ReapDeadSessions(); len(reaped) > 0 {
				s.broadcastSessions()
			}
			if len(s.svc.Sessions()) == 0 {
				continue
			}
			// Per-session driver Observers own the polling logic. Each
			// observer decides whether to capture-pane, compute hash, or
			// no-op (event-driven drivers like Claude). All writes go
			// through state.Store, so this loop has nothing to merge.
			s.svc.Observers.Tick(t)
			s.broadcastSessions()

			titleTick++
			if titleTick >= metaResolveCycle {
				titleTick = 0
				if s.svc.ResolveAgentMeta() {
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

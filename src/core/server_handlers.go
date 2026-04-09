package core

import (
	"log/slog"
	"time"

	"github.com/take/agent-roost/session/driver"
)

// IPC dispatch handlers. Each runs on the handleConn goroutine that
// produced the command, NOT on the server actor — they may freely call
// Coordinator methods (which have their own actor) and queue outbox
// writes via sendTo / sendResponse.

// handleSubscribe wires a client into the broadcast pipeline. Three
// outbox writes are performed inside ONE server-actor closure to fix
// the race that the previous implementation had:
//
//   1. response (subscribe ack)
//   2. initial sessions snapshot
//   3. broadcastEnabled = true
//
// Performing all three under the actor lock means any concurrent
// broadcast cannot interleave between the snapshot and the flag flip,
// so the TUI always sees: response → initial snapshot → subsequent
// broadcasts (in the order broadcasts were submitted to the actor).
//
// Building the snapshot calls Coordinator.SnapshotSessionsAndActive,
// which routes through the Coordinator actor; that work happens BEFORE
// we re-enter the server actor so the server-actor closure stays
// fast and never blocks on Coordinator (which would invert the actor
// dependency edge).
func (s *Server) handleSubscribe(cc *clientConn) {
	snapshot := s.buildSessionsEvent(false)
	response := Message{Type: "response"}
	s.exec(func() {
		s.sendTo(cc, response)
		s.sendTo(cc, snapshot)
		cc.broadcastEnabled = true
	})
	slog.Info("subscriber connected")
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

// handleAgentEvent routes a hook event to the Coordinator. Server does
// NOT broadcast sessions-changed for hook events: the Coordinator fires
// its own broadcast via the registered notifier inside HandleHookEvent
// once the driver has consumed the event. Doing it here too would
// deliver the same payload twice.
func (s *Server) handleAgentEvent(cc *clientConn, args map[string]string) {
	ev := driver.AgentEventFromArgs(args)
	slog.Debug("agent event received",
		"type", ev.Type, "session", ev.SessionID,
		"state", ev.State, "log", ev.Log)
	switch ev.Type {
	case driver.AgentEventSessionStart:
		s.coord.HandleHookEvent(ev)
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
		s.sendResponse(cc, Message{})
	default:
		s.sendError(cc, "unknown agent event type: "+string(ev.Type))
	}
}

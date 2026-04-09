package core

import (
	"os"

	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
)

// AgentEventResult reports the consequences of applying an agent hook event:
// the driver-defined agent identity (used by callers that need to look up an
// AgentSession by ID), and flags for whether the AgentStore binding or the
// persisted DriverState changed.
type AgentEventResult struct {
	Identity     string
	BindChanged  bool
	StateChanged bool
}

// ApplyAgentEvent merges the driver state carried by an agent hook event into
// the session identified by ev.Pane (or the currently active session if Pane
// is empty). The driver-supplied IdentityKey decides which DriverState entry
// is the agent identity used for AgentStore binding.
//
// This is the single core entry point for hook payloads: core never reads
// driver-specific keys directly, it just hands the bag to the driver.
func (s *Service) ApplyAgentEvent(ev driver.AgentEvent) AgentEventResult {
	windowID := s.ResolveWindowID(ev.Pane)
	if windowID == "" {
		return AgentEventResult{}
	}
	sess := s.Manager.FindByWindowID(windowID)
	if sess == nil {
		return AgentEventResult{}
	}
	res := AgentEventResult{}
	if key := s.Drivers.Get(sess.Command).IdentityKey(); key != "" {
		res.Identity = ev.DriverState[key]
		if res.Identity != "" {
			res.BindChanged = s.AgentStore.Bind(windowID, res.Identity)
		}
	}
	res.StateChanged = s.Manager.MergeDriverState(windowID, ev.DriverState)
	return res
}

// HandleStatusLine updates the agent status line by agentSessionID.
// If the agent is bound to the active session, syncs to tmux.
func (s *Service) HandleStatusLine(agentSessionID, line string) bool {
	changed := s.AgentStore.UpdateStatusLine(agentSessionID, line)
	if s.syncStatus != nil && s.activeWindowID != "" {
		active := s.AgentStore.GetByWindow(s.activeWindowID)
		if active != nil && active.ID == agentSessionID {
			s.syncStatus(line)
		}
	}
	return changed
}

// TranscriptPathByAgent returns the absolute transcript file path for the
// given agent session, by asking the session's driver to interpret the
// stored DriverState bag.
func (s *Service) TranscriptPathByAgent(agentSessionID string) string {
	wid := s.AgentStore.WindowIDByAgent(agentSessionID)
	if wid == "" {
		return ""
	}
	sess := s.Manager.FindByWindowID(wid)
	if sess == nil {
		return ""
	}
	return s.transcriptPathFor(sess)
}

// transcriptPathFor delegates the entire path-resolution decision to the
// session's driver. Priority among reported vs. computed paths is the
// driver's responsibility (Claude prefers a hook-reported path).
func (s *Service) transcriptPathFor(sess *session.Session) string {
	home, _ := os.UserHomeDir()
	return s.Drivers.Get(sess.Command).TranscriptFilePath(home, driver.SessionContext{
		Command:     sess.Command,
		Project:     sess.Project,
		DriverState: sess.DriverState,
	})
}

// UpdateStatusFromTranscript reads new transcript content and updates the
// status line.
func (s *Service) UpdateStatusFromTranscript(agentSessionID string) bool {
	path := s.TranscriptPathByAgent(agentSessionID)
	line, changed := s.Tracker.Update(agentSessionID, path)
	if changed {
		s.HandleStatusLine(agentSessionID, line)
	}
	return changed
}

// ResolveAgentMeta resolves metadata from agent transcript files for windows
// that already have a known agent session binding. Unbound windows are
// skipped — binding only happens through hook events that carry pane context,
// since guessing from "newest .jsonl in project dir" causes multiple sessions
// in the same project to collapse onto a single agent session.
func (s *Service) ResolveAgentMeta() bool {
	fsys := os.DirFS("/")
	home, _ := os.UserHomeDir()
	changed := false
	for _, sess := range s.Manager.All() {
		agentID := s.AgentStore.IDByWindow(sess.WindowID)
		if agentID == "" {
			continue
		}
		meta := s.Drivers.Get(sess.Command).ResolveMeta(fsys, home, driver.SessionContext{
			Command:     sess.Command,
			Project:     sess.Project,
			DriverState: sess.DriverState,
		})
		if meta.Title == "" && meta.LastPrompt == "" && len(meta.Subjects) == 0 {
			continue
		}
		if s.AgentStore.UpdateMeta(agentID, meta) {
			changed = true
		}
	}
	return changed
}

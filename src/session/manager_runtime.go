package session

import "log/slog"

// This file groups the runtime-mutation methods that update agent-reported
// fields on a Session and persist them to tmux user options. Pulled out of
// manager.go to keep that file under the 500-line house limit.

// SetAgentSessionID writes the @roost_agent_session user option for the given
// window and updates the in-memory cache. Returns true if the value changed.
func (m *Manager) SetAgentSessionID(windowID, agentSessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.WindowID == windowID {
			if s.AgentSessionID == agentSessionID {
				return false
			}
			if err := m.tmux.SetWindowUserOption(windowID, "@roost_agent_session", agentSessionID); err != nil {
				slog.Error("set agent session option failed", "window", windowID, "err", err)
				return false
			}
			s.AgentSessionID = agentSessionID
			m.saveSnapshotLocked()
			return true
		}
	}
	return false
}

// SetAgentWorkingDir writes the @roost_agent_workdir user option for the
// given window and updates the in-memory cache. Returns true if the working
// dir OR the derived branch tag changed. The working dir is the actual
// directory the agent process is running in (driver-neutral concept — Claude
// reports it via the hook `cwd` field, but other drivers may source it
// differently). For worktree-style invocations this differs from sess.Project,
// and roost uses it as the source of truth for git branch detection.
func (m *Manager) SetAgentWorkingDir(windowID, workingDir string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.WindowID == windowID {
			if s.AgentWorkingDir == workingDir {
				return false
			}
			if err := m.tmux.SetWindowUserOption(windowID, "@roost_agent_workdir", workingDir); err != nil {
				slog.Error("set agent workdir option failed", "window", windowID, "err", err)
				return false
			}
			s.AgentWorkingDir = workingDir
			// Branch detection target shifts from Project to AgentWorkingDir,
			// so re-derive tags now that we know where the agent really lives.
			m.refreshSessionBranchLocked(s)
			m.saveSnapshotLocked()
			return true
		}
	}
	return false
}

// SetAgentTranscriptPath writes the @roost_agent_transcript user option for
// the given window and updates the in-memory cache. Returns true when the
// value changed. The transcript path is the absolute path the agent itself
// reports via hook events; roost stores it verbatim and prefers it over any
// path it could compute, since the agent is the canonical source.
func (m *Manager) SetAgentTranscriptPath(windowID, transcriptPath string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.WindowID == windowID {
			if s.AgentTranscriptPath == transcriptPath {
				return false
			}
			if err := m.tmux.SetWindowUserOption(windowID, "@roost_agent_transcript", transcriptPath); err != nil {
				slog.Error("set agent transcript option failed", "window", windowID, "err", err)
				return false
			}
			s.AgentTranscriptPath = transcriptPath
			m.saveSnapshotLocked()
			return true
		}
	}
	return false
}

// RefreshBranch re-detects the git branch for the given session and updates
// the @roost_tags user option if it changed.
func (m *Manager) RefreshBranch(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.ID == sessionID {
			return m.refreshSessionBranchLocked(s)
		}
	}
	return false
}

func (m *Manager) refreshSessionBranchLocked(s *Session) bool {
	target := s.AgentWorkingDir
	if target == "" {
		target = s.Project
	}
	tags := buildTags(m.detectBranch(target))
	if tagsEqual(s.Tags, tags) {
		return false
	}
	if err := m.tmux.SetWindowUserOption(s.WindowID, "@roost_tags", encodeTags(tags)); err != nil {
		slog.Warn("refresh branch: set tags failed", "window", s.WindowID, "err", err)
		return false
	}
	s.Tags = tags
	m.saveSnapshotLocked()
	return true
}

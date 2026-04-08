package driver

import "sync"

type AgentStore struct {
	mu       sync.RWMutex
	sessions map[string]*AgentSession
	bindings map[string]string // windowID → agent session ID
}

func NewAgentStore() *AgentStore {
	return &AgentStore{
		sessions: make(map[string]*AgentSession),
		bindings: make(map[string]string),
	}
}

func (s *AgentStore) Bind(windowID, agentSessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bindLocked(windowID, agentSessionID)
}

func (s *AgentStore) bindLocked(windowID, agentSessionID string) bool {
	oldID := s.bindings[windowID]
	s.bindings[windowID] = agentSessionID
	if _, ok := s.sessions[agentSessionID]; !ok {
		s.sessions[agentSessionID] = &AgentSession{
			ID:    agentSessionID,
			State: AgentStateUnset,
		}
	}
	return oldID != agentSessionID
}

// RestoreFromBindings rehydrates the store from a windowID → agentSessionID
// map. Used at Coordinator startup to restore bindings persisted in tmux
// window options without going through the hook event path.
func (s *AgentStore) RestoreFromBindings(bindings map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for windowID, agentID := range bindings {
		if windowID == "" || agentID == "" {
			continue
		}
		s.bindLocked(windowID, agentID)
	}
}

func (s *AgentStore) Unbind(windowID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.bindings, windowID)
}

func (s *AgentStore) Get(agentSessionID string) *AgentSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[agentSessionID]
}

func (s *AgentStore) GetByWindow(windowID string) *AgentSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.bindings[windowID]
	if !ok {
		return nil
	}
	return s.sessions[id]
}

func (s *AgentStore) UpdateState(agentSessionID string, state AgentState) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[agentSessionID]
	if !ok {
		return false
	}
	if sess.State == state {
		return false
	}
	sess.State = state
	return true
}

func (s *AgentStore) UpdateStatusLine(agentSessionID string, line string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[agentSessionID]
	if !ok {
		return false
	}
	if sess.StatusLine == line {
		return false
	}
	sess.StatusLine = line
	return true
}

// UpdateTranscriptPath sets the absolute JSONL transcript path for the given
// agent session. The agent session is created on demand so this can be called
// before any explicit Bind (e.g. when a hook event arrives without a prior
// SessionStart). Returns true when the value changed.
func (s *AgentStore) UpdateTranscriptPath(agentSessionID, path string) bool {
	if agentSessionID == "" || path == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[agentSessionID]
	if !ok {
		sess = &AgentSession{ID: agentSessionID, State: AgentStateUnset}
		s.sessions[agentSessionID] = sess
	}
	if sess.TranscriptPath == path {
		return false
	}
	sess.TranscriptPath = path
	return true
}

func (s *AgentStore) IDByWindow(windowID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bindings[windowID]
}

func (s *AgentStore) WindowIDByAgent(agentSessionID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for wid, aid := range s.bindings {
		if aid == agentSessionID {
			return wid
		}
	}
	return ""
}

func (s *AgentStore) UpdateMeta(agentSessionID string, meta SessionMeta) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[agentSessionID]
	if !ok {
		return false
	}
	changed := false
	if meta.Title != "" && sess.Title != meta.Title {
		sess.Title = meta.Title
		changed = true
	}
	if meta.LastPrompt != "" && sess.LastPrompt != meta.LastPrompt {
		sess.LastPrompt = meta.LastPrompt
		changed = true
	}
	if len(meta.Subjects) > 0 && !slicesEqual(sess.Subjects, meta.Subjects) {
		sess.Subjects = meta.Subjects
		changed = true
	}
	if meta.AgentName != "" && sess.AgentName != meta.AgentName {
		sess.AgentName = meta.AgentName
		changed = true
	}
	if sess.CurrentTool != meta.CurrentTool {
		sess.CurrentTool = meta.CurrentTool
		changed = true
	}
	if !slicesEqual(sess.RecentCommands, meta.RecentCommands) {
		sess.RecentCommands = meta.RecentCommands
		changed = true
	}
	if sess.ErrorCount != meta.ErrorCount {
		sess.ErrorCount = meta.ErrorCount
		changed = true
	}
	if !slicesEqual(sess.TouchedFiles, meta.TouchedFiles) {
		sess.TouchedFiles = meta.TouchedFiles
		changed = true
	}
	if !mapsEqual(sess.SubagentCounts, meta.SubagentCounts) {
		sess.SubagentCounts = meta.SubagentCounts
		changed = true
	}
	return changed
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func mapsEqual(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/take/agent-roost/lib/git"
)

// TmuxClient is the subset of tmux operations Manager needs to manage roost
// sessions backed by tmux window user options.
type TmuxClient interface {
	NewWindow(name, command, startDir string) (string, error)
	KillWindow(windowID string) error
	SetOption(target, key, value string) error
	SetWindowUserOption(windowID, key, value string) error
	SetWindowUserOptions(windowID string, kv map[string]string) error
	ListRoostWindows() ([]RoostWindow, error)
}

// Manager keeps an in-memory cache of roost sessions reconstructed from tmux
// window user options. The cache is rebuilt by Refresh() and updated in-place
// by Create/Stop/SetAgentSessionID/RefreshBranch.
type Manager struct {
	tmux         TmuxClient
	dataDir      string
	detectBranch func(string) string
	mu           sync.RWMutex
	sessions     []*Session
}

func NewManager(t TmuxClient, dataDir string) *Manager {
	return &Manager{
		tmux:         t,
		dataDir:      dataDir,
		detectBranch: git.DetectBranch,
	}
}

// Refresh rebuilds the in-memory cache from tmux user options. It is a pure
// read operation — branch synchronization is the caller's responsibility
// (see SyncBranches).
func (m *Manager) Refresh() error {
	slog.Info("refreshing sessions")
	windows, err := m.tmux.ListRoostWindows()
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.sessions = m.sessions[:0]
	for _, w := range windows {
		m.sessions = append(m.sessions, windowToSession(w))
	}
	return nil
}

// SyncBranches re-detects the git branch for every session and writes
// changes back to the @roost_tags user option.
func (m *Manager) SyncBranches() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		m.refreshSessionBranchLocked(s)
	}
}

func (m *Manager) Create(project, command string) (*Session, error) {
	id, err := generateID()
	if err != nil {
		return nil, err
	}
	slog.Info("creating session", "project", project, "command", command, "id", id)

	tags := buildTags(m.detectBranch(project))
	createdAt := time.Now()

	name := filepath.Base(project) + ":" + id
	windowID, err := m.tmux.NewWindow(name, "exec "+command, project)
	if err != nil {
		slog.Error("create: window failed", "err", err)
		return nil, err
	}
	if err := m.tmux.SetOption(windowID, "remain-on-exit", "on"); err != nil {
		slog.Warn("create: set remain-on-exit failed", "err", err)
	}

	options := map[string]string{
		"@roost_id":         id,
		"@roost_project":    project,
		"@roost_command":    command,
		"@roost_created_at": createdAt.UTC().Format(time.RFC3339),
		"@roost_tags":       encodeTags(tags),
	}
	if err := m.tmux.SetWindowUserOptions(windowID, options); err != nil {
		slog.Error("create: set window options failed", "err", err)
		m.tmux.KillWindow(windowID)
		return nil, err
	}

	s := &Session{
		ID:        id,
		Project:   project,
		Command:   command,
		WindowID:  windowID,
		CreatedAt: createdAt,
		State:     StateRunning,
		Tags:      tags,
	}

	m.mu.Lock()
	m.sessions = append(m.sessions, s)
	m.mu.Unlock()
	slog.Info("session created", "id", id, "window", windowID)
	return s, nil
}

func (m *Manager) Stop(sessionID string) error {
	slog.Info("stopping session", "id", sessionID)
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, s := range m.sessions {
		if s.ID == sessionID {
			if err := m.tmux.KillWindow(s.WindowID); err != nil {
				return err
			}
			m.sessions = append(m.sessions[:i], m.sessions[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *Manager) Clear() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		m.tmux.KillWindow(s.WindowID)
	}
	m.sessions = nil
	return nil
}

func (m *Manager) All() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Session, len(m.sessions))
	copy(out, m.sessions)
	return out
}

func (m *Manager) ByProject() map[string][]*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	grouped := make(map[string][]*Session)
	for _, s := range m.sessions {
		key := s.Name()
		grouped[key] = append(grouped[key], s)
	}
	return grouped
}

func (m *Manager) FindByWindowID(windowID string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.sessions {
		if s.WindowID == windowID {
			return s
		}
	}
	return nil
}

func (m *Manager) FindByID(id string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.sessions {
		if s.ID == id {
			return s
		}
	}
	return nil
}

func (m *Manager) UpdateStates(states map[string]State) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for _, s := range m.sessions {
		if st, ok := states[s.WindowID]; ok {
			if s.State != st {
				s.StateChangedAt = now
			}
			s.State = st
		}
	}
}

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
			return true
		}
	}
	return false
}

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
	tags := buildTags(m.detectBranch(s.Project))
	if tagsEqual(s.Tags, tags) {
		return false
	}
	if err := m.tmux.SetWindowUserOption(s.WindowID, "@roost_tags", encodeTags(tags)); err != nil {
		slog.Warn("refresh branch: set tags failed", "window", s.WindowID, "err", err)
		return false
	}
	s.Tags = tags
	return true
}

func (m *Manager) DataDir() string {
	return m.dataDir
}

func buildTags(branch string) []Tag {
	if branch == "" {
		return nil
	}
	return []Tag{{Text: branch, Foreground: "#A9DC76"}}
}

func tagsEqual(a, b []Tag) bool {
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

func encodeTags(tags []Tag) string {
	if len(tags) == 0 {
		return ""
	}
	data, err := json.Marshal(tags)
	if err != nil {
		return ""
	}
	return string(data)
}

func decodeTags(s string) []Tag {
	if s == "" {
		return nil
	}
	var tags []Tag
	if err := json.Unmarshal([]byte(s), &tags); err != nil {
		return nil
	}
	return tags
}

func windowToSession(w RoostWindow) *Session {
	createdAt, _ := time.Parse(time.RFC3339, w.CreatedAt)
	return &Session{
		ID:             w.ID,
		Project:        w.Project,
		Command:        w.Command,
		WindowID:       w.WindowID,
		AgentSessionID: w.AgentSessionID,
		CreatedAt:      createdAt,
		Tags:           decodeTags(w.Tags),
	}
}

func generateID() (string, error) {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}


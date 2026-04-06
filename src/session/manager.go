package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type TmuxClient interface {
	NewWindow(name, command, startDir string) (string, error)
	KillWindow(windowID string) error
	ListWindowIDs() ([]string, error)
	SetOption(target, key, value string) error
}

type Manager struct {
	tmux     TmuxClient
	dataDir  string
	mu       sync.RWMutex
	sessions []*Session
}

func NewManager(tmux TmuxClient, dataDir string) *Manager {
	return &Manager{
		tmux:    tmux,
		dataDir: dataDir,
	}
}

func (m *Manager) Refresh() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.load(); err != nil {
		return err
	}
	return m.reconcile()
}

func (m *Manager) Create(project, command string) (*Session, error) {
	id, err := generateID()
	if err != nil {
		return nil, err
	}

	name := filepath.Base(project) + ":" + id
	windowID, err := m.tmux.NewWindow(name, "cd "+project+" && "+command, project)
	if err != nil {
		return nil, err
	}
	m.tmux.SetOption(windowID, "remain-on-exit", "on")

	logFile, err := os.Create(LogPath(m.dataDir, id))
	if err != nil {
		return nil, err
	}
	logFile.Close()

	s := &Session{
		ID:        id,
		Project:   project,
		Command:   command,
		WindowID:  windowID,
		CreatedAt: time.Now(),
		State:     StateRunning,
	}

	m.mu.Lock()
	m.sessions = append(m.sessions, s)
	err = m.save()
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (m *Manager) Stop(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, s := range m.sessions {
		if s.ID == sessionID {
			if err := m.tmux.KillWindow(s.WindowID); err != nil {
				return err
			}
			m.sessions = append(m.sessions[:i], m.sessions[i+1:]...)
			return m.save()
		}
	}
	return nil
}

func (m *Manager) Clear() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions = nil
	return m.save()
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
	for _, s := range m.sessions {
		if st, ok := states[s.WindowID]; ok {
			s.State = st
		}
	}
}

func (m *Manager) DataDir() string {
	return m.dataDir
}

func (m *Manager) load() error {
	data, err := os.ReadFile(m.filePath())
	if os.IsNotExist(err) {
		m.sessions = []*Session{}
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &m.sessions)
}

func (m *Manager) save() error {
	data, err := json.MarshalIndent(m.sessions, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := m.filePath() + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, m.filePath())
}

func (m *Manager) reconcile() error {
	ids, err := m.tmux.ListWindowIDs()
	if err != nil {
		return err
	}
	alive := make(map[string]bool, len(ids))
	for _, id := range ids {
		alive[id] = true
	}

	changed := false
	filtered := m.sessions[:0]
	for _, s := range m.sessions {
		if alive[s.WindowID] {
			filtered = append(filtered, s)
		} else {
			changed = true
		}
	}
	m.sessions = filtered

	if changed {
		return m.save()
	}
	return nil
}

func (m *Manager) filePath() string {
	return filepath.Join(m.dataDir, "sessions.json")
}

func generateID() (string, error) {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

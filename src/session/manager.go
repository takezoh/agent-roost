package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/take/agent-roost/config"
)

type TmuxClient interface {
	NewWindow(name, command, startDir string) (string, error)
	KillWindow(windowID string) error
	ListWindowIDs() ([]string, error)
	SetOption(target, key, value string) error
}

type Manager struct {
	sessions []*Session
	filePath string
	tmux     TmuxClient
	cfg      *config.Config
}

func NewManager(tmuxClient TmuxClient, cfg *config.Config) (*Manager, error) {
	m := &Manager{
		filePath: sessionsFilePath(),
		tmux:     tmuxClient,
		cfg:      cfg,
	}
	if err := m.Load(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) Load() error {
	data, err := os.ReadFile(m.filePath)
	if os.IsNotExist(err) {
		m.sessions = []*Session{}
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &m.sessions)
}

func (m *Manager) Save() error {
	data, err := json.MarshalIndent(m.sessions, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := m.filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, m.filePath)
}

func (m *Manager) Create(project, command string) (*Session, error) {
	id, err := generateID()
	if err != nil {
		return nil, err
	}

	name := filepath.Base(project)
	windowID, err := m.tmux.NewWindow(name, "cd "+project+" && "+command, project)
	if err != nil {
		return nil, err
	}
	m.tmux.SetOption(windowID, "remain-on-exit", "on")

	logFile, err := os.Create(LogPath(id))
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

	m.sessions = append(m.sessions, s)
	if err := m.Save(); err != nil {
		return nil, err
	}
	return s, nil
}

func (m *Manager) Stop(sessionID string) error {
	for i, s := range m.sessions {
		if s.ID == sessionID {
			if err := m.tmux.KillWindow(s.WindowID); err != nil {
				return err
			}
			m.sessions = append(m.sessions[:i], m.sessions[i+1:]...)
			return m.Save()
		}
	}
	return nil
}

func (m *Manager) All() []*Session {
	out := make([]*Session, len(m.sessions))
	copy(out, m.sessions)
	return out
}

func (m *Manager) ByProject() map[string][]*Session {
	grouped := make(map[string][]*Session)
	for _, s := range m.sessions {
		key := s.Name()
		grouped[key] = append(grouped[key], s)
	}
	return grouped
}

func (m *Manager) FindByID(id string) *Session {
	for _, s := range m.sessions {
		if s.ID == id {
			return s
		}
	}
	return nil
}

func (m *Manager) UpdateStates(states map[string]State) {
	for _, s := range m.sessions {
		if st, ok := states[s.WindowID]; ok {
			s.State = st
		}
	}
}

func (m *Manager) Reconcile() error {
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
		return m.Save()
	}
	return nil
}

func sessionsFilePath() string {
	return filepath.Join(config.ConfigDir(), "sessions.json")
}

func generateID() (string, error) {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

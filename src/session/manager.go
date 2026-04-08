package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/take/agent-roost/lib/git"
	"github.com/take/agent-roost/session/driver"
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

// Refresh rebuilds the in-memory cache from tmux user options and writes the
// result to the cold-boot snapshot. Branch synchronization is the caller's
// responsibility (see SyncBranches).
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
	m.saveSnapshotLocked()
	return nil
}

// Recreate loads sessions.json and re-creates each entry as a new tmux window.
// Used at Coordinator startup when client.SessionExists() returned false (PC
// reboot scenario), after setupNewSession() has constructed a fresh tmux
// session. Persisted WindowIDs are discarded — tmux assigns new ones — but
// AgentSessionID values are restored on the new windows so old conversation
// metadata stays accessible. The driver registry is consulted for the spawn
// command so e.g. Claude is started with "claude --resume <id>" to pick up
// the previous conversation transcript.
func (m *Manager) Recreate(drivers *driver.Registry) error {
	snapshot, err := m.loadSnapshot()
	if err != nil {
		return err
	}
	if len(snapshot) == 0 {
		return nil
	}
	slog.Info("recreating sessions from snapshot", "count", len(snapshot))

	m.mu.Lock()
	defer m.mu.Unlock()

	m.sessions = m.sessions[:0]
	for _, s := range snapshot {
		spawn := drivers.Get(s.Command).SpawnCommand(s.Command, s.AgentSessionID)
		name := filepath.Base(s.Project) + ":" + s.ID
		// Worktree sessions need to be respawned inside the worktree dir,
		// not the original launch dir. AgentWorkingDir is the agent's own
		// reported cwd (set by hook), which equals the worktree path for
		// `claude --worktree` invocations.
		startDir := s.Project
		if s.AgentWorkingDir != "" {
			startDir = s.AgentWorkingDir
		}
		windowID, err := m.tmux.NewWindow(name, "exec "+spawn, startDir)
		if err != nil {
			slog.Error("recreate: NewWindow failed", "id", s.ID, "err", err)
			continue
		}
		if err := m.tmux.SetOption(windowID, "remain-on-exit", "off"); err != nil {
			slog.Warn("recreate: set remain-on-exit failed", "err", err)
		}
		s.WindowID = windowID
		if err := m.tmux.SetWindowUserOptions(windowID, sessionUserOptions(s)); err != nil {
			slog.Error("recreate: SetWindowUserOptions failed", "id", s.ID, "err", err)
			m.tmux.KillWindow(windowID)
			continue
		}
		s.State = StateRunning
		m.sessions = append(m.sessions, s)
	}
	m.saveSnapshotLocked()
	return nil
}

// sessionUserOptions converts a Session into the @roost_* user options that
// represent its runtime truth in tmux. Empty optional fields are omitted so
// the parsing path on read can distinguish "unset" from explicit empty.
func sessionUserOptions(s *Session) map[string]string {
	opts := map[string]string{
		"@roost_id":         s.ID,
		"@roost_project":    s.Project,
		"@roost_command":    s.Command,
		"@roost_created_at": s.CreatedAt.UTC().Format(time.RFC3339),
		"@roost_tags":       encodeTags(s.Tags),
	}
	if s.AgentSessionID != "" {
		opts["@roost_agent_session"] = s.AgentSessionID
	}
	if s.AgentWorkingDir != "" {
		opts["@roost_agent_workdir"] = s.AgentWorkingDir
	}
	if s.AgentTranscriptPath != "" {
		opts["@roost_agent_transcript"] = s.AgentTranscriptPath
	}
	return opts
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

	s := &Session{
		ID:        id,
		Project:   project,
		Command:   command,
		CreatedAt: time.Now(),
		State:     StateRunning,
		Tags:      buildTags(m.detectBranch(project)),
	}

	name := filepath.Base(project) + ":" + id
	windowID, err := m.tmux.NewWindow(name, "exec "+command, project)
	if err != nil {
		slog.Error("create: window failed", "err", err)
		return nil, err
	}
	if err := m.tmux.SetOption(windowID, "remain-on-exit", "off"); err != nil {
		slog.Warn("create: set remain-on-exit failed", "err", err)
	}
	s.WindowID = windowID
	if err := m.tmux.SetWindowUserOptions(windowID, sessionUserOptions(s)); err != nil {
		slog.Error("create: set window options failed", "err", err)
		m.tmux.KillWindow(windowID)
		return nil, err
	}

	m.mu.Lock()
	m.sessions = append(m.sessions, s)
	m.saveSnapshotLocked()
	m.mu.Unlock()
	slog.Info("session created", "id", id, "window", windowID)
	return s, nil
}

// RemovedSession describes a session whose tmux window has disappeared and
// has been evicted from the in-memory cache by ReconcileWindows.
type RemovedSession struct {
	ID       string
	WindowID string
}

// ReconcileWindows compares the in-memory cache against the live tmux window
// list and removes sessions whose windows no longer exist (typically because
// the agent process exited and tmux auto-killed the pane). Runtime fields on
// surviving sessions are preserved. Returns the removed entries so callers
// can clean up their own state (active window, agent bindings, etc.).
func (m *Manager) ReconcileWindows() ([]RemovedSession, error) {
	windows, err := m.tmux.ListRoostWindows()
	if err != nil {
		return nil, err
	}
	live := make(map[string]struct{}, len(windows))
	for _, w := range windows {
		live[w.WindowID] = struct{}{}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var removed []RemovedSession
	kept := m.sessions[:0]
	for _, s := range m.sessions {
		if _, ok := live[s.WindowID]; ok {
			kept = append(kept, s)
			continue
		}
		slog.Info("reconcile: session window gone", "id", s.ID, "window", s.WindowID)
		removed = append(removed, RemovedSession{ID: s.ID, WindowID: s.WindowID})
	}
	m.sessions = kept
	if len(removed) > 0 {
		m.saveSnapshotLocked()
	}
	return removed, nil
}

// KillWindow forcibly destroys a tmux window. Used by Service.handleActiveDeadPane
// to clean up the session window after swap-pane has moved a dead agent pane
// back into it.
func (m *Manager) KillWindow(windowID string) error {
	return m.tmux.KillWindow(windowID)
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
			m.saveSnapshotLocked()
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
	m.saveSnapshotLocked()
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

func (m *Manager) DataDir() string {
	return m.dataDir
}

// snapshotPath returns the cold-boot snapshot file path. The snapshot is a
// backup that lets the Coordinator rebuild sessions after the tmux server
// itself is gone (PC reboot, tmux kill-server). At runtime tmux user options
// remain the source of truth.
func (m *Manager) snapshotPath() string {
	return filepath.Join(m.dataDir, "sessions.json")
}

// saveSnapshotLocked writes the in-memory session list to sessions.json.
// Caller must hold m.mu. Errors are logged but not propagated — the snapshot
// is only consulted on cold boot, never during normal runtime.
func (m *Manager) saveSnapshotLocked() {
	// Marshal an explicit empty slice rather than a nil slice so that an
	// empty session list serializes as "[]" instead of "null".
	sessions := m.sessions
	if sessions == nil {
		sessions = []*Session{}
	}
	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		slog.Error("snapshot marshal failed", "err", err)
		return
	}
	tmp := m.snapshotPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		slog.Error("snapshot write failed", "err", err)
		return
	}
	if err := os.Rename(tmp, m.snapshotPath()); err != nil {
		slog.Error("snapshot rename failed", "err", err)
	}
}

// loadSnapshot reads sessions.json. Returns (nil, nil) if the file does not
// exist (fresh install / no prior sessions).
func (m *Manager) loadSnapshot() ([]*Session, error) {
	data, err := os.ReadFile(m.snapshotPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var sessions []*Session
	if err := json.Unmarshal(data, &sessions); err != nil {
		return nil, err
	}
	return sessions, nil
}

func buildTags(branch string) []Tag {
	if branch == "" {
		return nil
	}
	return []Tag{{Text: branch, Background: "#A9DC76"}}
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
		ID:                  w.ID,
		Project:             w.Project,
		Command:             w.Command,
		WindowID:            w.WindowID,
		AgentSessionID:      w.AgentSessionID,
		AgentWorkingDir:     w.AgentWorkingDir,
		AgentTranscriptPath: w.AgentTranscriptPath,
		CreatedAt:           createdAt,
		Tags:                decodeTags(w.Tags),
	}
}

func generateID() (string, error) {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}


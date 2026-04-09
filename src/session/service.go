package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// TmuxClient is the subset of tmux operations SessionService needs.
// Defined here (not in tmux/) so the session package never imports tmux,
// avoiding an import cycle. Coordinator wires the concrete *tmux.Client.
type TmuxClient interface {
	NewWindow(name, command, startDir string) (string, error)
	KillWindow(windowID string) error
	SetOption(target, key, value string) error
	SetWindowUserOption(windowID, key, value string) error
	SetWindowUserOptions(windowID string, kv map[string]string) error
	ListRoostWindows() ([]RoostWindow, error)
	DisplayMessage(target, format string) (string, error)
}

// SessionService keeps an in-memory cache of roost sessions reconstructed
// from tmux window user options. The cache is rebuilt by Refresh() and
// updated in-place by Create / Stop / UpdatePersistedState.
//
// SessionService never references DriverService or any Driver instance —
// the only correlation between the two services is the sessionID, which
// Coordinator uses to bind them together. Likewise SessionService no
// longer concerns itself with branch detection or any per-session UI
// state — those live in the Driver instance and surface via View().
type SessionService struct {
	tmux    TmuxClient
	dataDir string

	mu       sync.RWMutex
	sessions []*Session
}

func NewSessionService(t TmuxClient, dataDir string) *SessionService {
	return &SessionService{
		tmux:    t,
		dataDir: dataDir,
	}
}

func (s *SessionService) DataDir() string {
	return s.dataDir
}

// Refresh rebuilds the in-memory cache from tmux user options and writes
// the result to the cold-boot snapshot.
func (s *SessionService) Refresh() error {
	slog.Info("refreshing sessions")
	windows, err := s.tmux.ListRoostWindows()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = s.sessions[:0]
	for _, w := range windows {
		s.sessions = append(s.sessions, windowToSession(w))
	}
	s.saveSnapshotLocked()
	return nil
}

// LoadSnapshot reads sessions.json. Returns nil, nil when the file does
// not exist (fresh install / no prior sessions).
func (s *SessionService) LoadSnapshot() ([]*Session, error) {
	return s.loadSnapshot()
}

// Create spawns a new tmux window for the given project + command and
// records the new Session. Returns the freshly created Session.
func (s *SessionService) Create(project, command string) (*Session, error) {
	id, err := generateID()
	if err != nil {
		return nil, err
	}
	slog.Info("creating session", "project", project, "command", command, "id", id)
	now := time.Now()
	sess := &Session{
		ID:        id,
		Project:   project,
		Command:   command,
		CreatedAt: now,
	}
	if err := s.spawnWindowLocked(sess, "exec "+command, project); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.sessions = append(s.sessions, sess)
	s.saveSnapshotLocked()
	s.mu.Unlock()
	slog.Info("session created", "id", id, "window", sess.WindowID, "pane", sess.AgentPaneID)
	return sess, nil
}

// Spawn creates a tmux window for an already-known Session (used by the
// cold-boot Recreate path where the driver supplies a custom resume
// command and start dir).
func (s *SessionService) Spawn(sess *Session, spawnCmd, startDir string) error {
	if err := s.spawnWindowLocked(sess, "exec "+spawnCmd, startDir); err != nil {
		return err
	}
	s.mu.Lock()
	s.sessions = append(s.sessions, sess)
	s.saveSnapshotLocked()
	s.mu.Unlock()
	return nil
}

// spawnWindowLocked is shared by Create and Spawn. It does NOT hold s.mu —
// callers acquire the write lock around the slice append themselves.
func (s *SessionService) spawnWindowLocked(sess *Session, command, startDir string) error {
	name := filepath.Base(sess.Project) + ":" + sess.ID
	windowID, err := s.tmux.NewWindow(name, command, startDir)
	if err != nil {
		slog.Error("create window failed", "err", err)
		return err
	}
	if err := s.tmux.SetOption(windowID, "remain-on-exit", "off"); err != nil {
		slog.Warn("set remain-on-exit failed", "err", err)
	}
	sess.WindowID = windowID
	sess.AgentPaneID = s.queryAgentPaneID(windowID)
	if err := s.tmux.SetWindowUserOptions(windowID, sessionUserOptions(sess)); err != nil {
		slog.Error("set window options failed", "err", err)
		s.tmux.KillWindow(windowID)
		return err
	}
	return nil
}

func (s *SessionService) queryAgentPaneID(windowID string) string {
	out, err := s.tmux.DisplayMessage(windowID+".0", "#{pane_id}")
	if err != nil {
		slog.Warn("query agent pane id failed", "window", windowID, "err", err)
		return ""
	}
	return strings.TrimSpace(out)
}

// Stop kills a session window and removes it from the cache. Returns the
// removed Session (or nil if not found) so Coordinator can destroy the
// matching Driver instance.
func (s *SessionService) Stop(sessionID string) (*Session, error) {
	slog.Info("stopping session", "id", sessionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sess := range s.sessions {
		if sess.ID == sessionID {
			if err := s.tmux.KillWindow(sess.WindowID); err != nil {
				return nil, err
			}
			s.sessions = append(s.sessions[:i], s.sessions[i+1:]...)
			s.saveSnapshotLocked()
			return sess, nil
		}
	}
	return nil, nil
}

// KillWindow forcibly destroys a tmux window without dropping anything from
// the in-memory cache. Used by the dead-pane reaper, which then runs
// ReconcileWindows to update the cache.
func (s *SessionService) KillWindow(windowID string) error {
	return s.tmux.KillWindow(windowID)
}

// RemovedSession describes a session whose tmux window has disappeared and
// has been evicted from the in-memory cache by ReconcileWindows.
type RemovedSession struct {
	ID       string
	WindowID string
}

// ReconcileWindows compares the in-memory cache against the live tmux
// window list and removes sessions whose windows no longer exist (typically
// because the agent process exited and tmux auto-killed the pane).
func (s *SessionService) ReconcileWindows() ([]RemovedSession, error) {
	windows, err := s.tmux.ListRoostWindows()
	if err != nil {
		return nil, err
	}
	live := make(map[string]struct{}, len(windows))
	for _, w := range windows {
		live[w.WindowID] = struct{}{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var removed []RemovedSession
	kept := s.sessions[:0]
	for _, sess := range s.sessions {
		if _, ok := live[sess.WindowID]; ok {
			kept = append(kept, sess)
			continue
		}
		slog.Info("reconcile: session window gone", "id", sess.ID, "window", sess.WindowID)
		removed = append(removed, RemovedSession{ID: sess.ID, WindowID: sess.WindowID})
	}
	s.sessions = kept
	if len(removed) > 0 {
		s.saveSnapshotLocked()
	}
	return removed, nil
}

// UpdatePersistedState writes the driver's opaque PersistedState bag to
// tmux user options + sessions.json. SessionService never inspects the
// keys — only the driver knows what they mean.
func (s *SessionService) UpdatePersistedState(sessionID string, persisted map[string]string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sessions {
		if sess.ID != sessionID {
			continue
		}
		if persistedEqual(sess.PersistedState, persisted) {
			return false
		}
		encoded := encodePersistedState(persisted)
		if err := s.tmux.SetWindowUserOption(sess.WindowID, "@roost_persisted_state", encoded); err != nil {
			slog.Error("set persisted_state option failed", "window", sess.WindowID, "err", err)
			return false
		}
		sess.PersistedState = clonePersisted(persisted)
		s.saveSnapshotLocked()
		return true
	}
	return false
}

func (s *SessionService) All() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Session, len(s.sessions))
	copy(out, s.sessions)
	return out
}

func (s *SessionService) ByProject() map[string][]*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	grouped := make(map[string][]*Session)
	for _, sess := range s.sessions {
		key := sess.Name()
		grouped[key] = append(grouped[key], sess)
	}
	return grouped
}

func (s *SessionService) FindByWindowID(windowID string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sess := range s.sessions {
		if sess.WindowID == windowID {
			return sess
		}
	}
	return nil
}

// FindByAgentPaneID looks up a session by its agent pane id. Pane ids are
// stable across swap-pane, so this lets the dead-pane reaper identify which
// session a dead pane belongs to regardless of which window currently
// hosts it.
func (s *SessionService) FindByAgentPaneID(paneID string) *Session {
	if paneID == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sess := range s.sessions {
		if sess.AgentPaneID == paneID {
			return sess
		}
	}
	return nil
}

func (s *SessionService) FindByID(id string) *Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sess := range s.sessions {
		if sess.ID == id {
			return sess
		}
	}
	return nil
}

// snapshotPath returns the cold-boot snapshot file path. The snapshot is
// a backup that lets the Coordinator rebuild sessions after the tmux
// server itself is gone (PC reboot, tmux kill-server). At runtime tmux
// user options remain the source of truth.
func (s *SessionService) snapshotPath() string {
	return filepath.Join(s.dataDir, "sessions.json")
}

func (s *SessionService) saveSnapshotLocked() {
	sessions := s.sessions
	if sessions == nil {
		sessions = []*Session{}
	}
	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		slog.Error("snapshot marshal failed", "err", err)
		return
	}
	tmp := s.snapshotPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		slog.Error("snapshot write failed", "err", err)
		return
	}
	if err := os.Rename(tmp, s.snapshotPath()); err != nil {
		slog.Error("snapshot rename failed", "err", err)
	}
}

func (s *SessionService) loadSnapshot() ([]*Session, error) {
	data, err := os.ReadFile(s.snapshotPath())
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

// sessionUserOptions converts a Session into the @roost_* user options that
// represent its static metadata in tmux. PersistedState is packed into a
// single JSON-encoded option so this layer never has to know which keys
// the driver uses; an empty bag is omitted entirely. Tags are not stored
// here — they live in the driver's PersistedState bag (driver-owned).
func sessionUserOptions(sess *Session) map[string]string {
	opts := map[string]string{
		"@roost_id":         sess.ID,
		"@roost_project":    sess.Project,
		"@roost_command":    sess.Command,
		"@roost_created_at": sess.CreatedAt.UTC().Format(time.RFC3339),
	}
	if sess.AgentPaneID != "" {
		opts["@roost_agent_pane"] = sess.AgentPaneID
	}
	if encoded := encodePersistedState(sess.PersistedState); encoded != "" {
		opts["@roost_persisted_state"] = encoded
	}
	return opts
}

func encodePersistedState(state map[string]string) string {
	if len(state) == 0 {
		return ""
	}
	data, err := json.Marshal(state)
	if err != nil {
		return ""
	}
	return string(data)
}

func decodePersistedState(s string) map[string]string {
	if s == "" {
		return nil
	}
	var state map[string]string
	if err := json.Unmarshal([]byte(s), &state); err != nil {
		return nil
	}
	if len(state) == 0 {
		return nil
	}
	return state
}

func clonePersisted(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func persistedEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

func windowToSession(w RoostWindow) *Session {
	createdAt, _ := time.Parse(time.RFC3339, w.CreatedAt)
	return &Session{
		ID:             w.ID,
		Project:        w.Project,
		Command:        w.Command,
		WindowID:       w.WindowID,
		AgentPaneID:    w.AgentPaneID,
		CreatedAt:      createdAt,
		PersistedState: decodePersistedState(w.PersistedState),
	}
}

func generateID() (string, error) {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

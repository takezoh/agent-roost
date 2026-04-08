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
	DisplayMessage(target, format string) (string, error)
}

// Manager keeps an in-memory cache of roost sessions reconstructed from tmux
// window user options. The cache is rebuilt by Refresh() and updated in-place
// by Create/Stop/MergeDriverState/RefreshBranch.
//
// drivers is consulted whenever Manager needs a driver-aware operation
// (working dir for branch detection, spawn command for cold-boot recreate).
// core treats DriverState as opaque; the driver registry is the only place
// inside Manager where driver-specific semantics are resolved.
type Manager struct {
	tmux         TmuxClient
	dataDir      string
	drivers      *driver.Registry
	detectBranch func(string) string
	mu           sync.RWMutex
	sessions     []*Session
}

func NewManager(t TmuxClient, dataDir string, drivers *driver.Registry) *Manager {
	return &Manager{
		tmux:         t,
		dataDir:      dataDir,
		drivers:      drivers,
		detectBranch: git.DetectBranch,
	}
}

// sessionContext builds the driver-visible projection of a session for
// passing to Driver methods.
func sessionContext(s *Session) driver.SessionContext {
	return driver.SessionContext{
		Command:     s.Command,
		Project:     s.Project,
		DriverState: s.DriverState,
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
// the entire DriverState bag is restored so the driver can resume the prior
// conversation (Claude uses its `session_id` key to add `--resume <id>`).
func (m *Manager) Recreate() error {
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
		d := m.drivers.Get(s.Command)
		ctx := sessionContext(s)
		spawn := d.SpawnCommand(s.Command, ctx)
		name := filepath.Base(s.Project) + ":" + s.ID
		// Worktree sessions need to be respawned inside the worktree dir,
		// not the original launch dir. The driver knows where the agent
		// process actually lives (Claude returns the cwd it received via
		// hook, which equals the worktree path for `claude --worktree`).
		startDir := s.Project
		if wd := d.WorkingDir(ctx); wd != "" {
			startDir = wd
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
		// AgentPaneID from the old tmux server is meaningless after a cold
		// boot — query the freshly spawned pane for its new id before
		// writing user options.
		s.AgentPaneID = m.queryAgentPaneID(windowID)
		// We just spawned a fresh agent process, so the persisted state is
		// stale — reset before writing options so tmux records the new
		// runtime, not whatever the previous Coordinator left behind.
		s.State = StateRunning
		s.StateChangedAt = time.Now()
		if err := m.tmux.SetWindowUserOptions(windowID, sessionUserOptions(s)); err != nil {
			slog.Error("recreate: SetWindowUserOptions failed", "id", s.ID, "err", err)
			m.tmux.KillWindow(windowID)
			continue
		}
		m.sessions = append(m.sessions, s)
	}
	m.saveSnapshotLocked()
	return nil
}

// sessionUserOptions converts a Session into the @roost_* user options that
// represent its runtime truth in tmux. DriverState is packed into a single
// JSON-encoded option so this layer never has to know which keys a driver
// uses; an empty driver state is omitted entirely.
func sessionUserOptions(s *Session) map[string]string {
	opts := map[string]string{
		"@roost_id":         s.ID,
		"@roost_project":    s.Project,
		"@roost_command":    s.Command,
		"@roost_created_at": s.CreatedAt.UTC().Format(time.RFC3339),
		"@roost_tags":       encodeTags(s.Tags),
		"@roost_state":      s.State.String(),
	}
	if s.AgentPaneID != "" {
		opts["@roost_agent_pane"] = s.AgentPaneID
	}
	if !s.StateChangedAt.IsZero() {
		opts["@roost_state_changed_at"] = s.StateChangedAt.UTC().Format(time.RFC3339)
	}
	if encoded := encodeDriverState(s.DriverState); encoded != "" {
		opts["@roost_driver_state"] = encoded
	}
	return opts
}

// encodeDriverState serializes a DriverState map to JSON. Empty maps return
// "" so callers can omit the option entirely.
func encodeDriverState(state map[string]string) string {
	if len(state) == 0 {
		return ""
	}
	data, err := json.Marshal(state)
	if err != nil {
		return ""
	}
	return string(data)
}

// decodeDriverState parses the JSON form written by encodeDriverState. Empty
// strings and parse failures yield nil so callers can rely on the standard
// nil-map semantics.
func decodeDriverState(s string) map[string]string {
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

	now := time.Now()
	s := &Session{
		ID:             id,
		Project:        project,
		Command:        command,
		CreatedAt:      now,
		State:          StateRunning,
		StateChangedAt: now,
		Tags:           buildTags(m.detectBranch(project)),
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
	s.AgentPaneID = m.queryAgentPaneID(windowID)
	if err := m.tmux.SetWindowUserOptions(windowID, sessionUserOptions(s)); err != nil {
		slog.Error("create: set window options failed", "err", err)
		m.tmux.KillWindow(windowID)
		return nil, err
	}

	m.mu.Lock()
	m.sessions = append(m.sessions, s)
	m.saveSnapshotLocked()
	m.mu.Unlock()
	slog.Info("session created", "id", id, "window", windowID, "pane", s.AgentPaneID)
	return s, nil
}

// queryAgentPaneID returns the tmux pane id (e.g. "%5") for pane 0 of the
// given window. Pane ids are stable across swap-pane, so storing this gives
// the reaper a way to identify a session by its agent pane regardless of
// where it currently lives in the tmux layout. Failures are non-fatal: an
// empty pane id just means the reaper falls back to ReconcileWindows-only
// cleanup for this session.
func (m *Manager) queryAgentPaneID(windowID string) string {
	out, err := m.tmux.DisplayMessage(windowID+":0.0", "#{pane_id}")
	if err != nil {
		slog.Warn("query agent pane id failed", "window", windowID, "err", err)
		return ""
	}
	return strings.TrimSpace(out)
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

// KillWindow forcibly destroys a tmux window. Used by Service.reapDeadPane00
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

// FindByAgentPaneID looks up a session by its agent pane id (e.g. "%5").
// Pane ids are stable across swap-pane, so this lets the reaper identify
// which session a dead pane belongs to regardless of which window currently
// hosts it. Sessions whose AgentPaneID is empty (e.g. legacy entries from
// before pane id tracking landed, or Create-time tmux failures) never match.
func (m *Manager) FindByAgentPaneID(paneID string) *Session {
	if paneID == "" {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.sessions {
		if s.AgentPaneID == paneID {
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
	stateChangedAt, _ := time.Parse(time.RFC3339, w.StateChangedAt)
	return &Session{
		ID:             w.ID,
		Project:        w.Project,
		Command:        w.Command,
		WindowID:       w.WindowID,
		AgentPaneID:    w.AgentPaneID,
		CreatedAt:      createdAt,
		Tags:           decodeTags(w.Tags),
		DriverState:    decodeDriverState(w.DriverState),
		State:          ParseState(w.State),
		StateChangedAt: stateChangedAt,
	}
}

func generateID() (string, error) {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}


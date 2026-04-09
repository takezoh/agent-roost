package state

import (
	"sync"
	"time"
)

// Tmux user option keys backing the persisted store.
const (
	OptionStatus    = "@roost_state"
	OptionChangedAt = "@roost_state_changed_at"
)

// OptionWriter is the minimal tmux capability the Store needs to persist
// status updates. Defined here so the state package never imports tmux
// directly — Coordinator wires the concrete client in via this interface.
type OptionWriter interface {
	SetWindowUserOptions(windowID string, kv map[string]string) error
	UnsetWindowUserOptions(windowID string, keys ...string) error
}

// OptionReader is the minimal capability the Store needs at startup to
// reconstruct itself from existing tmux user options.
type OptionReader interface {
	// ListWindowOptions returns a windowID → (key → value) map for every
	// roost-managed window. Implementations only include the options that
	// belong to the state package; metadata options are out of scope here.
	ListWindowOptions() (map[string]map[string]string, error)
}

// Store is the single source of truth for per-session dynamic status.
// All writes flow through Set, which persists synchronously to tmux first
// (I/O 先行・状態変更後行) and only updates the in-memory cache on success.
type Store interface {
	Get(windowID string) (Info, bool)
	Set(windowID string, info Info) error
	Delete(windowID string) error
	Snapshot() map[string]Info
	LoadFromTmux(reader OptionReader) error
}

// NewStore returns a Store backed by an in-memory map and persisted to tmux
// via the given writer.
func NewStore(writer OptionWriter) Store {
	return &store{
		writer: writer,
		data:   make(map[string]Info),
	}
}

type store struct {
	writer OptionWriter

	mu   sync.RWMutex
	data map[string]Info
}

func (s *store) Get(windowID string) (Info, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	info, ok := s.data[windowID]
	return info, ok
}

func (s *store) Set(windowID string, info Info) error {
	if windowID == "" {
		return nil
	}
	if err := s.writer.SetWindowUserOptions(windowID, map[string]string{
		OptionStatus:    info.Status.String(),
		OptionChangedAt: info.ChangedAt.UTC().Format(time.RFC3339),
	}); err != nil {
		return err
	}
	s.mu.Lock()
	s.data[windowID] = info
	s.mu.Unlock()
	return nil
}

func (s *store) Delete(windowID string) error {
	if windowID == "" {
		return nil
	}
	if err := s.writer.UnsetWindowUserOptions(windowID, OptionStatus, OptionChangedAt); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.data, windowID)
	s.mu.Unlock()
	return nil
}

func (s *store) Snapshot() map[string]Info {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]Info, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out
}

// LoadFromTmux populates the in-memory cache from tmux user options at
// Coordinator startup. Windows whose @roost_state option is missing or
// unparseable are skipped — there is no fallback.
func (s *store) LoadFromTmux(reader OptionReader) error {
	options, err := reader.ListWindowOptions()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for windowID, opts := range options {
		statusStr := opts[OptionStatus]
		if statusStr == "" {
			continue
		}
		status, ok := ParseStatus(statusStr)
		if !ok {
			continue
		}
		changedAt, _ := time.Parse(time.RFC3339, opts[OptionChangedAt])
		s.data[windowID] = Info{
			Status:    status,
			ChangedAt: changedAt,
		}
	}
	return nil
}

package session

import "log/slog"

// This file groups the runtime-mutation methods that update agent-reported
// driver state on a Session and persist it to tmux user options. Pulled out
// of manager.go to keep that file under the 500-line house limit.

// MergeDriverState merges the given updates into the session's DriverState
// map and persists the new state to the @roost_driver_state tmux user option
// (and the cold-boot snapshot). An empty value in updates deletes the key.
//
// Returns true when the merged state differs from the current state. The
// caller is responsible for triggering a branch refresh after a successful
// merge if the driver's working dir might have changed: Manager exposes
// RefreshBranch for that purpose so this method stays driver-agnostic.
func (m *Manager) MergeDriverState(windowID string, updates map[string]string) bool {
	if windowID == "" || len(updates) == 0 {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.WindowID != windowID {
			continue
		}
		merged, changed := mergeDriverStateMap(s.DriverState, updates)
		if !changed {
			return false
		}
		encoded := encodeDriverState(merged)
		if err := m.tmux.SetWindowUserOption(windowID, "@roost_driver_state", encoded); err != nil {
			slog.Error("set driver_state option failed", "window", windowID, "err", err)
			return false
		}
		s.DriverState = merged
		// Branch detection target may have shifted (e.g. Claude reported a
		// new working dir), so re-derive tags now that DriverState changed.
		m.refreshSessionBranchLocked(s)
		m.saveSnapshotLocked()
		return true
	}
	return false
}

// mergeDriverStateMap returns the result of applying updates onto current
// (without mutating either) and a flag indicating whether anything changed.
// Empty values in updates delete the key.
func mergeDriverStateMap(current, updates map[string]string) (map[string]string, bool) {
	merged := make(map[string]string, len(current)+len(updates))
	for k, v := range current {
		merged[k] = v
	}
	changed := false
	for k, v := range updates {
		if v == "" {
			if _, ok := merged[k]; ok {
				delete(merged, k)
				changed = true
			}
			continue
		}
		if existing, ok := merged[k]; !ok || existing != v {
			merged[k] = v
			changed = true
		}
	}
	if len(merged) == 0 {
		return nil, changed
	}
	return merged, changed
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
	target := ""
	if m.drivers != nil {
		target = m.drivers.Get(s.Command).WorkingDir(sessionContext(s))
	}
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

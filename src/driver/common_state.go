package driver

import (
	"time"

	"github.com/takezoh/agent-roost/state"
)

// CommonState contains the shared fields and logic used by multiple
// agent drivers (Claude, Codex, Gemini, Generic). Embedding this struct
// ensures consistent state management across different driver implementations.
type CommonState struct {
	state.DriverStateBase

	// Identity & Context
	RoostSessionID string
	StartDir       string
	TranscriptPath string
	WorktreeName   string

	// Status bookkeeping
	Status          state.Status
	StatusChangedAt time.Time

	// Branch & Git context
	BranchTag          string
	BranchBG           string
	BranchFG           string
	BranchTarget       string
	BranchAt           time.Time
	BranchIsWorktree   bool
	BranchParentBranch string
	BranchInFlight     bool

	// Event & Prompt history
	LastPrompt           string
	LastAssistantMessage string
	LastHookEvent        string
	LastHookAt           time.Time

	// Summary & Display
	Summary         string
	SummaryInFlight bool
	Title           string

	// Hang detection: pane-capture hash comparison for background sessions.
	CaptureInFlight bool
	PaneHash        string    // SHA256 of last captured pane content
	PaneHashAt      time.Time // when PaneHash last changed (or first set)
	HangDetected    bool      // set when hang threshold fires; cleared on next hook
}

const (
	// commonBranchRefreshInterval is the minimum time between VCS branch
	// detections for an active session.
	commonBranchRefreshInterval = 30 * time.Second

	// commonHangThreshold is the time without pane changes or hooks
	// before an agent is considered stale and transitioned to Idle.
	commonHangThreshold = 120 * time.Second
)

// HandleTick common implementation for drivers. Completes StartDir,
// skips heavy work for Idle/Stopped sessions, refreshes branch info
// when active, and checks for hang conditions when running in the background.
func (c *CommonState) HandleTick(e state.DEvTick, hasActiveSubagents bool) []state.Effect {
	if c.StartDir == "" {
		c.StartDir = e.Project
	}

	if c.Status == state.StatusIdle || c.Status == state.StatusStopped {
		return nil
	}

	var effs []state.Effect

	// Branch refresh: only when the session is active (swapped into 0.0)
	// and the cache is stale or the working dir changed.
	if e.Active {
		target := c.StartDir
		if target == "" {
			target = e.Project
		}
		if target != "" && !c.BranchInFlight {
			if target != c.BranchTarget || e.Now.Sub(c.BranchAt) >= commonBranchRefreshInterval {
				c.BranchInFlight = true
				c.BranchTarget = target
				effs = append(effs, state.EffStartJob{
					Input: BranchDetectInput{WorkingDir: target},
				})
			}
		}
	}

	// Pane capture for hang detection: background Running sessions only.
	if !e.Active && c.Status == state.StatusRunning && e.PaneTarget != "" && !c.CaptureInFlight {
		c.CaptureInFlight = true
		effs = append(effs, state.EffStartJob{
			Input: CapturePaneInput{PaneTarget: e.PaneTarget, NLines: 5},
		})
	}

	// Hang threshold check: if Running, pane is primed, no subagents
	// are active, and neither pane content nor hook events have changed.
	if c.Status == state.StatusRunning && c.PaneHash != "" && !hasActiveSubagents {
		lastActivity := c.PaneHashAt
		if c.StatusChangedAt.After(lastActivity) {
			lastActivity = c.StatusChangedAt
		}
		if e.Now.Sub(lastActivity) > commonHangThreshold {
			c.Status = state.StatusIdle
			c.StatusChangedAt = e.Now
			c.HangDetected = true
			effs = append(effs, state.EffEventLogAppend{
				Line: "HangDetected (pane unchanged)",
			})
		}
	}

	return effs
}

// HandleCapturePaneResult updates the pane hash baseline.
func (c *CommonState) HandleCapturePaneResult(r CapturePaneResult, now time.Time) {
	c.CaptureInFlight = false
	if c.PaneHash == "" {
		c.PaneHash = r.Hash
		c.PaneHashAt = now
	} else if r.Hash != c.PaneHash {
		c.PaneHash = r.Hash
		c.PaneHashAt = now
	}
}

// ResetHangDetection clears hang state, restarting the timer from scratch.
func (c *CommonState) ResetHangDetection() {
	c.HangDetected = false
	c.PaneHash = ""
}

// Common persistence keys shared across drivers.
const (
	keyRoostSessionID     = "roost_session_id"
	keyStartDir           = "start_dir"
	keyTranscriptPath     = "transcript_path"
	keyWorktreeName       = "worktree_name"
	keyStatus             = "status"
	keyStatusChangedAt    = "status_changed_at"
	keyBranchTag          = "branch_tag"
	keyBranchBG           = "branch_bg"
	keyBranchFG           = "branch_fg"
	keyBranchTarget       = "branch_target"
	keyBranchAt           = "branch_at"
	keyBranchIsWorktree   = "branch_is_worktree"
	keyBranchParentBranch = "branch_parent_branch"
	keySummary            = "summary"
	keyTitle              = "title"
	keyLastPrompt           = "last_prompt"
	keyLastAssistantMessage = "last_assistant_message"
	keyLastHookEvent        = "last_hook_event"
	keyLastHookAt           = "last_hook_at"
)

// PersistCommon writes the shared fields of CommonState into the persistence bag.
func (c *CommonState) PersistCommon(out map[string]string) {
	if c.RoostSessionID != "" {
		out[keyRoostSessionID] = c.RoostSessionID
	}
	if c.StartDir != "" {
		out[keyStartDir] = c.StartDir
	}
	if c.TranscriptPath != "" {
		out[keyTranscriptPath] = c.TranscriptPath
	}
	if c.WorktreeName != "" {
		out[keyWorktreeName] = c.WorktreeName
	}
	out[keyStatus] = c.Status.String()
	if !c.StatusChangedAt.IsZero() {
		out[keyStatusChangedAt] = c.StatusChangedAt.UTC().Format(time.RFC3339)
	}
	if c.BranchTag != "" {
		out[keyBranchTag] = c.BranchTag
	}
	if c.BranchBG != "" {
		out[keyBranchBG] = c.BranchBG
	}
	if c.BranchFG != "" {
		out[keyBranchFG] = c.BranchFG
	}
	if c.BranchTarget != "" {
		out[keyBranchTarget] = c.BranchTarget
	}
	if !c.BranchAt.IsZero() {
		out[keyBranchAt] = c.BranchAt.UTC().Format(time.RFC3339)
	}
	if c.BranchIsWorktree {
		out[keyBranchIsWorktree] = "1"
	}
	if c.BranchParentBranch != "" {
		out[keyBranchParentBranch] = c.BranchParentBranch
	}
	if c.Summary != "" {
		out[keySummary] = c.Summary
	}
	if c.Title != "" {
		out[keyTitle] = c.Title
	}
	if c.LastPrompt != "" {
		out[keyLastPrompt] = c.LastPrompt
	}
	if c.LastAssistantMessage != "" {
		out[keyLastAssistantMessage] = c.LastAssistantMessage
	}
	if c.LastHookEvent != "" {
		out[keyLastHookEvent] = c.LastHookEvent
	}
	if !c.LastHookAt.IsZero() {
		out[keyLastHookAt] = c.LastHookAt.UTC().Format(time.RFC3339)
	}
}

// RestoreCommon rehydrates the shared fields of CommonState from the persistence bag.
func (c *CommonState) RestoreCommon(bag map[string]string) {
	if len(bag) == 0 {
		return
	}
	c.RoostSessionID = bag[keyRoostSessionID]
	c.StartDir = bag[keyStartDir]
	c.TranscriptPath = bag[keyTranscriptPath]
	c.WorktreeName = bag[keyWorktreeName]
	if v := bag[keyStatus]; v != "" {
		if status, ok := state.ParseStatus(v); ok {
			c.Status = status
		}
	}
	if v := bag[keyStatusChangedAt]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			c.StatusChangedAt = t
		}
	}
	c.BranchTag = bag[keyBranchTag]
	c.BranchBG = bag[keyBranchBG]
	c.BranchFG = bag[keyBranchFG]
	c.BranchTarget = bag[keyBranchTarget]
	if v := bag[keyBranchAt]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			c.BranchAt = t
		}
	}
	c.BranchIsWorktree = bag[keyBranchIsWorktree] == "1"
	c.BranchParentBranch = bag[keyBranchParentBranch]
	c.Summary = bag[keySummary]
	c.Title = bag[keyTitle]
	c.LastPrompt = bag[keyLastPrompt]
	c.LastAssistantMessage = bag[keyLastAssistantMessage]
	c.LastHookEvent = bag[keyLastHookEvent]
	if v := bag[keyLastHookAt]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			c.LastHookAt = t
		}
	}
}

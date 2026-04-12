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
	WorkingDir     string
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
}

// Common persistence keys shared across drivers.
const (
	keyRoostSessionID     = "roost_session_id"
	keyWorkingDir         = "working_dir"
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
	if c.WorkingDir != "" {
		out[keyWorkingDir] = c.WorkingDir
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
	c.WorkingDir = bag[keyWorkingDir]
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

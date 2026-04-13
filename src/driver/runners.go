package driver

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"

	roostgit "github.com/takezoh/agent-roost/lib/git"
	"github.com/takezoh/agent-roost/lib/vcs"
	"github.com/takezoh/agent-roost/runtime/worker"
	"github.com/takezoh/agent-roost/state"
)

var _ state.DriverState = GenericState{}

func RegisterRunners(capturePaneFn func(string, int) (string, error), language, summarizeCmd string) {
	setSummaryPromptLanguage(language)
	worker.RegisterRunner("capture_pane", newCapturePane(capturePaneFn))
	tp, hs := newTranscriptSummaryRunners(summarizeCmd)
	worker.RegisterRunner("transcript_parse", tp)
	worker.RegisterRunner("codex_transcript_parse", newCodexTranscriptParse())
	worker.RegisterRunner("summary_command", hs)
	worker.RegisterRunner("branch_detect", newBranchDetect())
	worker.RegisterRunner("worktree_setup", newWorktreeSetup())
}

func newCapturePane(captureFunc func(string, int) (string, error)) func(CapturePaneInput) (CapturePaneResult, error) {
	return func(in CapturePaneInput) (CapturePaneResult, error) {
		content, err := captureFunc(in.PaneTarget, in.NLines)
		if err != nil {
			return CapturePaneResult{}, err
		}
		h := sha256.Sum256([]byte(content))
		return CapturePaneResult{
			Content: content,
			Hash:    hex.EncodeToString(h[:]),
		}, nil
	}
}

func newBranchDetect() func(BranchDetectInput) (BranchDetectResult, error) {
	return func(in BranchDetectInput) (BranchDetectResult, error) {
		r := vcs.DetectBranch(in.WorkingDir)
		return BranchDetectResult{
			Branch: r.Branch, Background: r.Background, Foreground: r.Foreground,
			IsWorktree: r.IsWorktree, ParentBranch: r.ParentBranch,
		}, nil
	}
}

func newWorktreeSetup() func(WorktreeSetupInput) (WorktreeSetupResult, error) {
	return func(in WorktreeSetupInput) (WorktreeSetupResult, error) {
		root, err := roostgit.RepoRoot(in.RepoDir)
		if err != nil {
			return WorktreeSetupResult{}, err
		}
		for _, name := range in.CandidateNames {
			if name == "" {
				continue
			}
			path := filepath.Join(root, ".roost", "worktrees", name)
			if _, err := os.Stat(path); err == nil {
				continue
			}
			dir, err := roostgit.CreateWorktree(in.RepoDir, name)
			if err == nil {
				return WorktreeSetupResult{WorkingDir: dir, Name: name}, nil
			}
		}
		return WorktreeSetupResult{}, errors.New("failed to create managed worktree")
	}
}

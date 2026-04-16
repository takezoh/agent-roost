package driver

import (
	"context"
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

func RegisterRunners(capturePaneFn func(string, int) (string, error), summarizeCmd, dataDir string) {
	worker.RegisterRunner("capture_pane", newCapturePane(capturePaneFn))
	tp, hs := newTranscriptSummaryRunners(summarizeCmd, dataDir)
	worker.RegisterRunner("transcript_parse", tp)
	worker.RegisterRunner("codex_transcript_parse", newCodexTranscriptParse())
	worker.RegisterRunner("summary_command", hs)
	worker.RegisterRunner("branch_detect", newBranchDetect())
	worker.RegisterRunner("worktree_setup", newWorktreeSetup())
}

func newCapturePane(captureFunc func(string, int) (string, error)) func(context.Context, CapturePaneInput) (CapturePaneResult, error) {
	return func(ctx context.Context, in CapturePaneInput) (CapturePaneResult, error) {
		if err := ctx.Err(); err != nil {
			return CapturePaneResult{}, err
		}
		// tmux layer is bounded via lib/tmux.Client.Run default timeout.
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

func newBranchDetect() func(context.Context, BranchDetectInput) (BranchDetectResult, error) {
	return func(ctx context.Context, in BranchDetectInput) (BranchDetectResult, error) {
		r := vcs.DetectBranch(ctx, in.WorkingDir)
		return BranchDetectResult{
			Branch: r.Branch, Background: r.Background, Foreground: r.Foreground,
			IsWorktree: r.IsWorktree, ParentBranch: r.ParentBranch,
		}, nil
	}
}

func newWorktreeSetup() func(context.Context, WorktreeSetupInput) (WorktreeSetupResult, error) {
	return func(ctx context.Context, in WorktreeSetupInput) (WorktreeSetupResult, error) {
		root, err := roostgit.RepoRoot(ctx, in.RepoDir)
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
			dir, err := roostgit.CreateWorktree(ctx, in.RepoDir, name)
			if err == nil {
				return WorktreeSetupResult{StartDir: dir, Name: name}, nil
			}
		}
		return WorktreeSetupResult{}, errors.New("failed to create managed worktree")
	}
}

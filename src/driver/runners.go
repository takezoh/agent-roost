package driver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/takezoh/agent-roost/driver/vt"
	roostgit "github.com/takezoh/agent-roost/lib/git"
	"github.com/takezoh/agent-roost/lib/vcs"
	"github.com/takezoh/agent-roost/runtime/worker"
	"github.com/takezoh/agent-roost/state"
)

var _ state.DriverState = GenericState{}

// RegisterRunners registers all worker-pool runners for the driver package.
// It returns an evict function that removes the VT terminal for a given pane
// target; callers should invoke it when a pane is unregistered to prevent
// unbounded terminal accumulation.
func RegisterRunners(capturePaneFn func(string, int) (string, error), summarizeCmd, dataDir string) (evict func(pane string)) {
	store := &terminalStore{entries: map[string]*vt.Terminal{}}
	worker.RegisterRunner("capture_pane", store.newRunner(capturePaneFn))
	tp, hs := newTranscriptSummaryRunners(summarizeCmd, dataDir)
	worker.RegisterRunner("transcript_parse", tp)
	worker.RegisterRunner("codex_transcript_parse", newCodexTranscriptParse())
	worker.RegisterRunner("summary_command", hs)
	worker.RegisterRunner("branch_detect", newBranchDetect())
	worker.RegisterRunner("worktree_setup", newWorktreeSetup())
	return store.evict
}

// terminalStore holds per-pane VT emulators protected by a mutex so that
// multiple worker-pool goroutines can access different panes concurrently
// without a data race. The mutex also guards each Terminal's state for the
// rare case where two capture jobs for the same pane overlap in flight.
type terminalStore struct {
	mu      sync.Mutex
	entries map[string]*vt.Terminal
}

// feedAndSnapshot feeds ANSI bytes into the terminal for pane and returns a
// Snapshot. The entire operation is mutex-guarded to prevent concurrent map
// access and concurrent Terminal mutations across worker-pool goroutines.
func (s *terminalStore) feedAndSnapshot(pane string, ansi []byte) vt.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.entries[pane]
	if !ok {
		t = vt.New(0, 0)
		s.entries[pane] = t
	}
	_ = t.Feed(ansi)
	return t.Snapshot()
}

// evict removes the terminal for a pane. Called when a session pane is
// unregistered so that the scrollback buffer is not held indefinitely.
func (s *terminalStore) evict(pane string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, pane)
}

func (s *terminalStore) newRunner(captureEscapedFn func(string, int) (string, error)) func(context.Context, CapturePaneInput) (CapturePaneResult, error) {
	return func(ctx context.Context, in CapturePaneInput) (CapturePaneResult, error) {
		if err := ctx.Err(); err != nil {
			return CapturePaneResult{}, err
		}
		ansiContent, err := captureEscapedFn(in.PaneTarget, in.NLines)
		if err != nil {
			return CapturePaneResult{}, err
		}
		snap := s.feedAndSnapshot(in.PaneTarget, []byte(ansiContent))
		return CapturePaneResult{
			Content:  ansiContent,
			Hash:     snap.Stable,
			Snapshot: snap,
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

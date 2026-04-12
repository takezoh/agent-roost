package driver

import (
	"crypto/sha256"
	"encoding/hex"

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
	worker.RegisterRunner("summary_command", hs)
	worker.RegisterRunner("branch_detect", newBranchDetect())
}

func newCapturePane(captureFunc func(string, int) (string, error)) func(CapturePaneInput) (CapturePaneResult, error) {
	return func(in CapturePaneInput) (CapturePaneResult, error) {
		content, err := captureFunc(in.WindowTarget, in.NLines)
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

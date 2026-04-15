package driver

import (
	"errors"
	"fmt"
	"strings"

	"github.com/takezoh/agent-roost/state"
)

// TmuxInjector abstracts the minimal tmux operations InjectPrompt needs.
// A concrete implementation backed by lib/tmux is a follow-up task.
type TmuxInjector interface {
	// ResolveFramePane returns the tmux pane target (e.g. "%3") registered
	// for the given frame id, or ("", false) if the frame is unknown.
	// The expected backing implementation reads the tmux session environment
	// variable ROOST_FRAME_<id> written by EffRegisterPane.
	ResolveFramePane(frameID state.FrameID) (string, bool)

	// PastePrompt writes text into the pane as a bracketed paste event.
	// The expected backing implementation uses:
	//
	//   tmux load-buffer -b <name> -   (with text on stdin)
	//   tmux paste-buffer -d -b <name> -t <target>
	//
	// Using paste-buffer instead of send-keys -l avoids the issue where
	// embedded newlines are interpreted as submit by Ink-based TUIs.
	PastePrompt(target, text string) error

	// SubmitEnter sends the Enter key to confirm the current input.
	//   tmux send-keys -t <target> Enter
	SubmitEnter(target string) error
}

// InjectPrompt pastes prompt into the tmux pane owned by frameID and
// submits it with Enter. This is the only reliable way to feed a prompt
// into an Ink-based TUI driver (Claude Code, Codex, Gemini) from an
// external process, since those TUIs ignore piped stdin.
//
// Trailing whitespace and newlines are stripped from prompt before
// sending; a trailing newline would otherwise cause a double-submit
// (paste end + Enter).
//
// Precondition: the target pane must be idle (waiting for input). Calling
// InjectPrompt while the TUI is generating a response will corrupt its
// input buffer. Idle detection is the caller's responsibility.
//
// Error semantics:
//   - empty or whitespace-only prompt: error, no tmux calls made
//   - unknown frame: error, no tmux calls made
//   - PastePrompt failure: error, SubmitEnter not called
//   - SubmitEnter failure: error, but the pasted text already sits in the
//     pane's input buffer; no cleanup attempt is made (tmux has no undo,
//     and sending Ctrl+U risks erasing the user's own input)
func InjectPrompt(inj TmuxInjector, frameID state.FrameID, prompt string) error {
	trimmed := strings.TrimRight(prompt, "\n\r\t ")
	if trimmed == "" {
		return errors.New("driver: empty prompt")
	}
	target, ok := inj.ResolveFramePane(frameID)
	if !ok || target == "" {
		return fmt.Errorf("driver: no pane for frame %q", frameID)
	}
	if err := inj.PastePrompt(target, trimmed); err != nil {
		return fmt.Errorf("driver: paste prompt: %w", err)
	}
	if err := inj.SubmitEnter(target); err != nil {
		return fmt.Errorf("driver: submit enter: %w", err)
	}
	return nil
}

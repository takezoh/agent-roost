package runtime

import (
	"fmt"

	"github.com/takezoh/agent-roost/state"
)

// RuntimeTmuxInjector implements driver.TmuxInjector backed by the
// runtime's session pane map and TmuxBackend.
type RuntimeTmuxInjector struct {
	panes map[state.FrameID]string // frameID → pane target
	tmux  TmuxBackend
}

// NewRuntimeTmuxInjector constructs an injector from the runtime's pane
// map and tmux backend.
func NewRuntimeTmuxInjector(panes map[state.FrameID]string, tmux TmuxBackend) *RuntimeTmuxInjector {
	return &RuntimeTmuxInjector{panes: panes, tmux: tmux}
}

// ResolveFramePane returns the pane target registered for frameID, or
// ("", false) if unknown or empty.
func (inj *RuntimeTmuxInjector) ResolveFramePane(frameID state.FrameID) (string, bool) {
	target, ok := inj.panes[frameID]
	return target, ok && target != ""
}

// PastePrompt loads text into a named buffer then pastes it into target.
func (inj *RuntimeTmuxInjector) PastePrompt(target, text string) error {
	bufName := fmt.Sprintf("roost-peer-%s", target)
	if err := inj.tmux.LoadBuffer(bufName, text); err != nil {
		return err
	}
	return inj.tmux.PasteBuffer(bufName, target)
}

// SubmitEnter sends the Enter key to target.
func (inj *RuntimeTmuxInjector) SubmitEnter(target string) error {
	return inj.tmux.SendEnter(target)
}

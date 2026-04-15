package driver

import (
	"errors"
	"strings"
	"testing"

	"github.com/takezoh/agent-roost/state"
)

// recordedCall captures one call made to fakeInjector.
type recordedCall struct {
	op     string // "resolve" | "paste" | "submit"
	frame  state.FrameID
	target string
	text   string
}

// fakeInjector is a test double for TmuxInjector.
type fakeInjector struct {
	panes     map[state.FrameID]string
	pasteErr  error
	submitErr error
	calls     []recordedCall
}

func (f *fakeInjector) ResolveFramePane(id state.FrameID) (string, bool) {
	f.calls = append(f.calls, recordedCall{op: "resolve", frame: id})
	p, ok := f.panes[id]
	return p, ok
}

func (f *fakeInjector) PastePrompt(target, text string) error {
	f.calls = append(f.calls, recordedCall{op: "paste", target: target, text: text})
	return f.pasteErr
}

func (f *fakeInjector) SubmitEnter(target string) error {
	f.calls = append(f.calls, recordedCall{op: "submit", target: target})
	return f.submitErr
}

func newFakeInjector(frameID state.FrameID, pane string) *fakeInjector {
	return &fakeInjector{
		panes: map[state.FrameID]string{frameID: pane},
	}
}

func TestInjectPrompt_Success(t *testing.T) {
	inj := newFakeInjector("frame1", "%3")
	err := InjectPrompt(inj, "frame1", "hello")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inj.calls) != 3 {
		t.Fatalf("want 3 calls, got %d: %v", len(inj.calls), inj.calls)
	}
	if inj.calls[0].op != "resolve" || inj.calls[0].frame != "frame1" {
		t.Errorf("call[0] = %v, want resolve frame1", inj.calls[0])
	}
	if inj.calls[1].op != "paste" || inj.calls[1].target != "%3" || inj.calls[1].text != "hello" {
		t.Errorf("call[1] = %v, want paste %%3 hello", inj.calls[1])
	}
	if inj.calls[2].op != "submit" || inj.calls[2].target != "%3" {
		t.Errorf("call[2] = %v, want submit %%3", inj.calls[2])
	}
}

func TestInjectPrompt_Multiline(t *testing.T) {
	inj := newFakeInjector("frame1", "%3")
	prompt := "line1\nline2\n  line3"
	err := InjectPrompt(inj, "frame1", prompt)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Multiline text is passed through unchanged (PastePrompt handles it).
	got := inj.calls[1].text
	if got != "line1\nline2\n  line3" {
		t.Errorf("paste text = %q, want original multiline", got)
	}
}

func TestInjectPrompt_TrimTrailing(t *testing.T) {
	inj := newFakeInjector("frame1", "%3")
	err := InjectPrompt(inj, "frame1", "hello\n\n")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := inj.calls[1].text
	if got != "hello" {
		t.Errorf("paste text = %q, want \"hello\"", got)
	}
}

func TestInjectPrompt_EmptyPrompt(t *testing.T) {
	inj := newFakeInjector("frame1", "%3")
	err := InjectPrompt(inj, "frame1", "")

	if err == nil {
		t.Fatal("want error for empty prompt, got nil")
	}
	if len(inj.calls) != 0 {
		t.Errorf("want no calls, got %v", inj.calls)
	}
}

func TestInjectPrompt_WhitespaceOnly(t *testing.T) {
	inj := newFakeInjector("frame1", "%3")
	err := InjectPrompt(inj, "frame1", "   \n\t")

	if err == nil {
		t.Fatal("want error for whitespace-only prompt, got nil")
	}
	if len(inj.calls) != 0 {
		t.Errorf("want no calls, got %v", inj.calls)
	}
}

func TestInjectPrompt_EmptyPaneTarget(t *testing.T) {
	inj := &fakeInjector{panes: map[state.FrameID]string{"frame1": ""}}
	err := InjectPrompt(inj, "frame1", "hello")

	if err == nil {
		t.Fatal("want error for empty pane target, got nil")
	}
	// resolve was called, but paste and submit were not.
	if len(inj.calls) != 1 || inj.calls[0].op != "resolve" {
		t.Errorf("want only resolve call, got %v", inj.calls)
	}
}

func TestInjectPrompt_UnknownFrame(t *testing.T) {
	inj := newFakeInjector("frame1", "%3")
	err := InjectPrompt(inj, "other-frame", "hello")

	if err == nil {
		t.Fatal("want error for unknown frame, got nil")
	}
	if !strings.Contains(err.Error(), "other-frame") {
		t.Errorf("error %q should mention the frame id", err.Error())
	}
	// resolve was called, but paste and submit were not.
	if len(inj.calls) != 1 || inj.calls[0].op != "resolve" {
		t.Errorf("want only resolve call, got %v", inj.calls)
	}
}

func TestInjectPrompt_PasteError(t *testing.T) {
	inj := newFakeInjector("frame1", "%3")
	inj.pasteErr = errors.New("pipe broken")
	err := InjectPrompt(inj, "frame1", "hello")

	if err == nil {
		t.Fatal("want error from paste, got nil")
	}
	if !strings.Contains(err.Error(), "pipe broken") {
		t.Errorf("error %q should wrap the paste error", err.Error())
	}
	// resolve + paste called, submit not called.
	ops := make([]string, len(inj.calls))
	for i, c := range inj.calls {
		ops[i] = c.op
	}
	want := []string{"resolve", "paste"}
	if strings.Join(ops, ",") != strings.Join(want, ",") {
		t.Errorf("calls = %v, want %v", ops, want)
	}
}

func TestInjectPrompt_SubmitError(t *testing.T) {
	inj := newFakeInjector("frame1", "%3")
	inj.submitErr = errors.New("session gone")
	err := InjectPrompt(inj, "frame1", "hello")

	if err == nil {
		t.Fatal("want error from submit, got nil")
	}
	if !strings.Contains(err.Error(), "session gone") {
		t.Errorf("error %q should wrap the submit error", err.Error())
	}
	// All three calls were made.
	if len(inj.calls) != 3 {
		t.Errorf("want 3 calls, got %d: %v", len(inj.calls), inj.calls)
	}
}

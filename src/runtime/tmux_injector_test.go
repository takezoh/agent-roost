package runtime

import (
	"errors"
	"testing"

	"github.com/takezoh/agent-roost/state"
)

// fakeTmux is a recording TmuxBackend for injector tests.
type fakeTmux struct {
	noopTmux
	loadBufferCalls  []loadBufferCall
	pasteBufferCalls []pasteBufferCall
	sendEnterCalls   []string
	loadErr          error
	pasteErr         error
	enterErr         error
}

type loadBufferCall struct{ name, text string }
type pasteBufferCall struct{ name, target string }

func (f *fakeTmux) LoadBuffer(name, text string) error {
	f.loadBufferCalls = append(f.loadBufferCalls, loadBufferCall{name, text})
	return f.loadErr
}

func (f *fakeTmux) PasteBuffer(name, target string) error {
	f.pasteBufferCalls = append(f.pasteBufferCalls, pasteBufferCall{name, target})
	return f.pasteErr
}

func (f *fakeTmux) SendEnter(target string) error {
	f.sendEnterCalls = append(f.sendEnterCalls, target)
	return f.enterErr
}

func TestRuntimeTmuxInjector_ResolveFramePane(t *testing.T) {
	panes := map[state.FrameID]string{
		"frame-1": "%5",
		"frame-2": "",
	}
	inj := NewRuntimeTmuxInjector(panes, &fakeTmux{})

	t.Run("known frame returns target and true", func(t *testing.T) {
		target, ok := inj.ResolveFramePane("frame-1")
		if !ok {
			t.Fatal("expected ok=true")
		}
		if target != "%5" {
			t.Fatalf("expected %%5, got %q", target)
		}
	})

	t.Run("empty target returns false", func(t *testing.T) {
		_, ok := inj.ResolveFramePane("frame-2")
		if ok {
			t.Fatal("expected ok=false for empty pane target")
		}
	})

	t.Run("unknown frame returns false", func(t *testing.T) {
		_, ok := inj.ResolveFramePane("no-such-frame")
		if ok {
			t.Fatal("expected ok=false for unknown frame")
		}
	})
}

func TestRuntimeTmuxInjector_PastePrompt(t *testing.T) {
	t.Run("calls LoadBuffer then PasteBuffer", func(t *testing.T) {
		ft := &fakeTmux{}
		inj := NewRuntimeTmuxInjector(nil, ft)

		if err := inj.PastePrompt("%5", "hello world"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ft.loadBufferCalls) != 1 {
			t.Fatalf("expected 1 LoadBuffer call, got %d", len(ft.loadBufferCalls))
		}
		if ft.loadBufferCalls[0].name != "roost-peer-%5" {
			t.Errorf("unexpected buffer name: %q", ft.loadBufferCalls[0].name)
		}
		if ft.loadBufferCalls[0].text != "hello world" {
			t.Errorf("unexpected buffer text: %q", ft.loadBufferCalls[0].text)
		}
		if len(ft.pasteBufferCalls) != 1 {
			t.Fatalf("expected 1 PasteBuffer call, got %d", len(ft.pasteBufferCalls))
		}
		if ft.pasteBufferCalls[0].name != "roost-peer-%5" {
			t.Errorf("unexpected paste buffer name: %q", ft.pasteBufferCalls[0].name)
		}
		if ft.pasteBufferCalls[0].target != "%5" {
			t.Errorf("unexpected paste target: %q", ft.pasteBufferCalls[0].target)
		}
	})

	t.Run("LoadBuffer error stops before PasteBuffer", func(t *testing.T) {
		ft := &fakeTmux{loadErr: errors.New("load failed")}
		inj := NewRuntimeTmuxInjector(nil, ft)

		if err := inj.PastePrompt("%5", "text"); err == nil {
			t.Fatal("expected error from LoadBuffer")
		}
		if len(ft.pasteBufferCalls) != 0 {
			t.Fatal("PasteBuffer must not be called when LoadBuffer fails")
		}
	})
}

func TestRuntimeTmuxInjector_SubmitEnter(t *testing.T) {
	ft := &fakeTmux{}
	inj := NewRuntimeTmuxInjector(nil, ft)

	if err := inj.SubmitEnter("%5"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ft.sendEnterCalls) != 1 {
		t.Fatalf("expected 1 SendEnter call, got %d", len(ft.sendEnterCalls))
	}
	if ft.sendEnterCalls[0] != "%5" {
		t.Errorf("unexpected SendEnter target: %q", ft.sendEnterCalls[0])
	}
}

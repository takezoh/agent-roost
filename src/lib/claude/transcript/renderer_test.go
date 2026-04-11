package transcript

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/takezoh/agent-roost/state"
)

func TestTabRendererAppend(t *testing.T) {
	cfg, _ := json.Marshal(RendererConfig{})
	r := state.NewTabRenderer(KindTranscript, cfg)
	if r == nil {
		t.Fatal("expected non-nil renderer for KindTranscript")
	}

	line := []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`)
	got := r.Append(line)
	if !strings.Contains(got, "hello") {
		t.Errorf("Append returned %q, want string containing 'hello'", got)
	}
}

func TestTabRendererReset(t *testing.T) {
	cfg, _ := json.Marshal(RendererConfig{})
	r := state.NewTabRenderer(KindTranscript, cfg)
	if r == nil {
		t.Fatal("expected non-nil renderer")
	}
	r.Reset()
}

func TestTabRendererShowThinkingViaConfig(t *testing.T) {
	cfg, _ := json.Marshal(RendererConfig{ShowThinking: true})
	r := state.NewTabRenderer(KindTranscript, cfg)
	if r == nil {
		t.Fatal("expected non-nil renderer")
	}

	line := []byte(`{"type":"assistant","message":{"content":[{"type":"thinking","text":"deep thought"}]}}`)
	got := r.Append(line)
	if !strings.Contains(got, "deep thought") {
		t.Errorf("with ShowThinking=true, Append returned %q, want 'deep thought'", got)
	}
}

func TestTabRendererThinkingHiddenByDefault(t *testing.T) {
	cfg, _ := json.Marshal(RendererConfig{})
	r := state.NewTabRenderer(KindTranscript, cfg)
	if r == nil {
		t.Fatal("expected non-nil renderer")
	}

	line := []byte(`{"type":"assistant","message":{"content":[{"type":"thinking","text":"secret"}]}}`)
	got := r.Append(line)
	if strings.Contains(got, "secret") {
		t.Errorf("thinking should be hidden by default, got %q", got)
	}
}

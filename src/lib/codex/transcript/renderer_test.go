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
		t.Fatal("expected renderer")
	}
	got := r.Append([]byte(`{"timestamp":"x","type":"event_msg","payload":{"type":"user_message","message":"hello","kind":"plain"}}`))
	if !strings.Contains(got, "[context] hello") {
		t.Fatalf("got %q", got)
	}
}

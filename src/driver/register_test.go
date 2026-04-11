package driver

import "testing"

func TestDecodeConfig(t *testing.T) {
	raw := map[string]any{"show_thinking": true}
	opts := decodeConfig[ClaudeOptions](raw)
	if !opts.ShowThinking {
		t.Error("ShowThinking should be true")
	}
}

func TestDecodeConfigEmpty(t *testing.T) {
	opts := decodeConfig[ClaudeOptions](nil)
	if opts.ShowThinking {
		t.Error("ShowThinking should default to false")
	}
}

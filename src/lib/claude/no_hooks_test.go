package claude

import (
	"testing"
)

func TestNoHooksEnvContainsSimpleFlag(t *testing.T) {
	env := NoHooksEnv()
	for _, kv := range env {
		if kv == "CLAUDE_CODE_SIMPLE=1" {
			return
		}
	}
	t.Errorf("expected CLAUDE_CODE_SIMPLE=1 in NoHooksEnv, got %v", env)
}

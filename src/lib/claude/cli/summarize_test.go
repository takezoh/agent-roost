package cli

import "testing"

func TestFilteredClaudeEnvStripsRoostSessionID(t *testing.T) {
	src := []string{
		"PATH=/usr/bin",
		"ROOST_SESSION_ID=should-be-dropped",
		"HOME=/home/take",
		"ANTHROPIC_API_KEY=keep-me",
	}
	out := filteredClaudeEnv(src)

	for _, kv := range out {
		if kv == "ROOST_SESSION_ID=should-be-dropped" {
			t.Errorf("ROOST_SESSION_ID leaked into filtered env: %v", out)
		}
	}

	mustKeep := []string{
		"PATH=/usr/bin",
		"HOME=/home/take",
		"ANTHROPIC_API_KEY=keep-me",
	}
	for _, want := range mustKeep {
		found := false
		for _, kv := range out {
			if kv == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q to survive filtering, got %v", want, out)
		}
	}
}

func TestFilteredClaudeEnvHandlesMalformedEntries(t *testing.T) {
	// An entry without `=` should be passed through unchanged (defensive
	// — os.Environ() never produces these on real systems but the helper
	// must not panic).
	src := []string{"PATH=/usr/bin", "MALFORMED_NO_EQUALS"}
	out := filteredClaudeEnv(src)
	if len(out) != 2 {
		t.Errorf("expected both entries preserved, got %v", out)
	}
}

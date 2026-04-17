package driver

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// summarizeWithCommand runs an arbitrary shell command as a one-shot
// summarizer. The prompt is written to the command's stdin; the trimmed
// stdout is returned as the summary. The command is executed via "sh -c"
// so shell features (pipes, env vars) work as expected.
func summarizeWithCommand(ctx context.Context, prompt, command string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Env = filteredRoostEnv(os.Environ())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("summarize command: %w (stderr=%s)", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stripHookLines(stdout.String())), nil
}

// filteredRoostEnv returns a copy of src with every ROOST_* env var
// removed, so a summarizer subprocess cannot impersonate a
// roost-tracked session. If a hook registered by `roost <agent> setup`
// still fires inside the summarizer and spawns `roost event <agent>`,
// that subprocess will see empty ROOST_FRAME_ID and the event is
// dropped at src/cli/event.go:32-36 without reaching the daemon.
func filteredRoostEnv(src []string) []string {
	out := make([]string, 0, len(src))
	for _, kv := range src {
		if strings.HasPrefix(kv, "ROOST_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// hookLineProducingPrefixes are stdout-prefixes that gemini (and similar
// CLIs) emit as part of post-response hook execution logging. They appear
// after the model's actual response and pollute the captured summary.
var hookLineProducingPrefixes = []string{
	"Created execution plan for ",
	"Expanding hook command:",
	"Hook execution for ",
}

// stripHookLines removes trailing lines that match any well-known hook-log
// prefix. Stops at the first non-matching, non-empty line — never touches
// content above the trailing block. Intentionally conservative: only fixed
// banner lines at the bottom are eaten, never the real summary body.
func stripHookLines(s string) string {
	lines := strings.Split(s, "\n")
	for len(lines) > 0 {
		last := strings.TrimRight(lines[len(lines)-1], " \t\r")
		if last == "" {
			lines = lines[:len(lines)-1]
			continue
		}
		match := false
		for _, p := range hookLineProducingPrefixes {
			if strings.HasPrefix(last, p) {
				match = true
				break
			}
		}
		if !match {
			break
		}
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

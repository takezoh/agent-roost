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
	return strings.TrimSpace(stdout.String()), nil
}

// filteredRoostEnv returns a copy of src with ROOST_SESSION_ID removed to
// prevent recursive roost hook events when the summarize command itself
// triggers a Claude/Codex/Gemini session.
func filteredRoostEnv(src []string) []string {
	drop := map[string]struct{}{
		"ROOST_SESSION_ID": {},
	}
	out := make([]string, 0, len(src))
	for _, kv := range src {
		i := strings.IndexByte(kv, '=')
		if i < 0 {
			out = append(out, kv)
			continue
		}
		if _, skip := drop[kv[:i]]; skip {
			continue
		}
		out = append(out, kv)
	}
	return out
}

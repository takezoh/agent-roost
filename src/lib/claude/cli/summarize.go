package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// SummarizeWithCommand runs an arbitrary shell command as a one-shot
// summarizer. The prompt is written to the command's stdin; the trimmed
// stdout is returned as the summary. The command is executed via "sh -c"
// so shell features (pipes, env vars) work as expected.
func SummarizeWithCommand(ctx context.Context, prompt, command string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("summarize command: %w (stderr=%s)", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// SummarizeWithHaiku runs `claude -p --model=haiku` as a one-shot
// background subprocess and returns the trimmed assistant output. The
// caller is expected to pass a bounded context — this package does not
// enforce a default timeout.
//
// Designed to be safe to invoke from inside an existing Claude Code
// session: the inner process is detached from the caller's project
// (cwd → temp dir, project settings skipped) and from the roost hook
// bridge (ROOST_SESSION_ID stripped) so it cannot recurse back into
// another summarizer. Auth (keychain / ANTHROPIC_API_KEY) and the
// user-level ~/.claude/settings.json are left intact so the call still
// authenticates.
func SummarizeWithHaiku(ctx context.Context, prompt string) (string, error) {
	cmd := exec.CommandContext(ctx, "claude",
		"-p",
		"--model=haiku",
		"--no-session-persistence",
		"--setting-sources", "user",
	)
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Dir = os.TempDir()
	cmd.Env = filteredClaudeEnv(os.Environ())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude -p haiku: %w (stderr=%s)", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// filteredClaudeEnv returns a copy of src with environment markers removed
// that would otherwise let the inner Claude invocation recurse the roost
// hook bridge (ROOST_SESSION_ID) back into another summarization. Auth
// vars (ANTHROPIC_*) and CLAUDECODE are intentionally NOT stripped — the
// inner CLI tolerates being run nested, and ANTHROPIC_API_KEY is required
// for non-keychain auth paths.
func filteredClaudeEnv(src []string) []string {
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

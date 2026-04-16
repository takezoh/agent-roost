package driver

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	libclaude "github.com/takezoh/agent-roost/lib/claude"
	libcodex "github.com/takezoh/agent-roost/lib/codex"
	libgemini "github.com/takezoh/agent-roost/lib/gemini"
)

// summarizeWithCommand runs an arbitrary shell command as a one-shot
// summarizer. The prompt is written to the command's stdin; the trimmed
// stdout is returned as the summary. The command is executed via "sh -c"
// so shell features (pipes, env vars) work as expected.
// dataDir is used to locate or create agent-specific no-hooks shadow files.
func summarizeWithCommand(ctx context.Context, prompt, command, dataDir string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Env = summarizeSubprocessEnv(dataDir, os.Environ())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("summarize command: %w (stderr=%s)", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// summarizeSubprocessEnv builds the env slice for a summarize subprocess:
// it strips all ROOST_* vars and injects per-agent hook-disable signals so
// that claude, gemini, and codex subprocesses do not fire their hooks.
func summarizeSubprocessEnv(dataDir string, src []string) []string {
	out := filteredRoostEnv(src)
	out = append(out, libclaude.NoHooksEnv()...)
	if dataDir != "" {
		if kv, err := libgemini.EnsureNoHooksSettings(dataDir); err != nil {
			slog.Warn("summarize: gemini no-hooks setup failed", "err", err)
		} else {
			out = append(out, kv)
		}
		if kv, err := libcodex.EnsureNoHooksHome(dataDir); err != nil {
			slog.Warn("summarize: codex no-hooks setup failed", "err", err)
		} else {
			out = append(out, kv)
		}
	}
	return out
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

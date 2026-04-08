package driver

import (
	"path/filepath"

	"github.com/mattn/go-shellwords"
)

// Kind returns the canonical driver name for a raw command string.
// It strips leading KEY=VALUE env assignments and any path prefix on the
// executable, so "FOO=bar /usr/local/bin/claude --worktree" yields "claude".
// Returns "" for empty or unparseable input.
func Kind(rawCommand string) string {
	if rawCommand == "" {
		return ""
	}
	_, args, err := shellwords.ParseWithEnvs(rawCommand)
	if err != nil || len(args) == 0 {
		return ""
	}
	return filepath.Base(args[0])
}

// Package cli holds Claude CLI invocation helpers.
//
// This package intentionally has zero internal dependencies so that
// session/driver/claude.go can import it without creating an import cycle.
// (lib/claude root depends on core, which depends on session/driver — so
// session/driver cannot import lib/claude root, only its leaf subpackages.)
package cli

import "strings"

// ResumeCommand returns the Claude CLI invocation that resumes a prior
// session by ID. Empty sessionID returns baseCommand unchanged. When
// resuming, --worktree is stripped because Claude treats it as "create a
// new worktree" and is incompatible with --resume; the caller is expected
// to start the process inside the existing worktree directory instead.
func ResumeCommand(baseCommand, sessionID string) string {
	if sessionID == "" {
		return baseCommand
	}
	return StripWorktreeFlag(baseCommand) + " --resume " + sessionID
}

// StripWorktreeFlag removes the Claude --worktree flag (and its optional
// name argument) from a command line. Recognized forms:
//
//	--worktree            (alone, or followed by another flag)
//	--worktree NAME       (space-separated name; NAME is dropped too)
//	--worktree=NAME       (single token form)
//
// Tokens that look like flags (start with "-") immediately after a bare
// --worktree are preserved, since they cannot be the worktree name.
func StripWorktreeFlag(command string) string {
	parts := strings.Fields(command)
	out := make([]string, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		if p == "--worktree" {
			if i+1 < len(parts) && !strings.HasPrefix(parts[i+1], "-") {
				i++ // drop the worktree name
			}
			continue
		}
		if strings.HasPrefix(p, "--worktree=") {
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, " ")
}

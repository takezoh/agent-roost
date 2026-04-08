// Package cli holds Claude CLI invocation helpers.
//
// This package intentionally has zero internal dependencies so that
// session/driver/claude.go can import it without creating an import cycle.
// (lib/claude root depends on core, which depends on session/driver — so
// session/driver cannot import lib/claude root, only its leaf subpackages.)
package cli

// ResumeCommand returns the Claude CLI invocation that resumes a prior
// session by ID. Empty sessionID returns baseCommand unchanged.
func ResumeCommand(baseCommand, sessionID string) string {
	if sessionID == "" {
		return baseCommand
	}
	return baseCommand + " --resume " + sessionID
}

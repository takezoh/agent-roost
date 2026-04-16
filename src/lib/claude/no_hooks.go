package claude

// NoHooksEnv returns env entries that prevent claude from running hooks
// when invoked as a summarize subprocess.
// CLAUDE_CODE_SIMPLE=1 is equivalent to the --bare flag and disables
// hooks, LSP integration, and plugin sync.
func NoHooksEnv() []string {
	return []string{"CLAUDE_CODE_SIMPLE=1"}
}

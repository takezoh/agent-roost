package core

import "strings"

// SetCommandAliases registers a map of command aliases. When create-session is
// called with a command that matches an alias name (after trimming), the value
// is substituted before the session is spawned. This is how shell-style
// aliases like clw="claude --worktree" are surfaced to roost without invoking
// an interactive shell.
func (s *Server) SetCommandAliases(aliases map[string]string) {
	s.aliases = aliases
}

// ResolveCommandAlias expands a command through the alias map. Unknown
// commands are returned trimmed but unchanged.
func ResolveCommandAlias(aliases map[string]string, command string) string {
	command = strings.TrimSpace(command)
	if expanded, ok := aliases[command]; ok {
		return expanded
	}
	return command
}

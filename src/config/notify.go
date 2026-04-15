package config

import (
	"fmt"
	"path"
)

// NotificationsConfig is the [notifications] table from settings.toml.
// An empty Rules slice means notifications are disabled.
//
// Example configuration:
//
//	# claude in ~/projects/prjA requests tool approval
//	[[notifications.rules]]
//	driver  = "claude"
//	project = "~/projects/prjA"
//	kind    = "pending_approval"
//
//	# any agent finishes its turn
//	[[notifications.rules]]
//	kind = "done"
//
//	# npm command completes
//	[[notifications.rules]]
//	command = "npm"
//	kind    = "done"
type NotificationsConfig struct {
	Rules []NotifyRule `toml:"rules"`
}

// NotifyRule describes a single notification trigger. All four fields
// are AND-combined: a notification fires only if every non-empty field
// matches. Empty string or "*" means "match any value" for that axis.
//
// Driver, Command, and Project patterns are evaluated with path.Match
// (glob syntax). The Project pattern is tilde-expanded before matching,
// so "~/projects/*" matches any project under ~/projects/.
type NotifyRule struct {
	Driver  string `toml:"driver"`  // glob; "" or "*" = any
	Command string `toml:"command"` // glob; "" or "*" = any
	Project string `toml:"project"` // glob; "" or "*" = any; "~" is expanded
	Kind    string `toml:"kind"`    // "pending_approval" | "done" | "" = any
}

// Matches reports whether this rule applies to the given event values.
// All four axes are AND-combined. Unknown kind values never match.
func (r NotifyRule) Matches(driver, command, project, kind string) bool {
	if !matchGlob(r.Driver, driver) {
		return false
	}
	if !matchGlob(r.Command, command) {
		return false
	}
	if !matchGlob(r.Project, project) {
		return false
	}
	if !matchKind(r.Kind, kind) {
		return false
	}
	return true
}

// AnyMatch reports whether at least one rule in the config matches.
// Returns false if Rules is empty (notifications disabled).
func (c *NotificationsConfig) AnyMatch(driver, command, project, kind string) bool {
	for _, rule := range c.Rules {
		if rule.Matches(driver, command, project, kind) {
			return true
		}
	}
	return false
}

// Validate checks that all glob patterns in every rule are syntactically
// valid. Returns an error describing the first invalid pattern found.
// Axes are checked in a fixed order (driver → command → project) so
// error messages are deterministic.
func (c *NotificationsConfig) Validate() error {
	for i, rule := range c.Rules {
		axes := [...]struct {
			name    string
			pattern string
		}{
			{"driver", rule.Driver},
			{"command", rule.Command},
			{"project", rule.Project},
		}
		for _, axis := range axes {
			if axis.pattern == "" || axis.pattern == "*" {
				continue
			}
			if _, err := path.Match(axis.pattern, ""); err != nil {
				return fmt.Errorf("notifications.rules[%d].%s: invalid glob %q: %w", i, axis.name, axis.pattern, err)
			}
		}
	}
	return nil
}

// matchGlob returns true when pattern is empty/"*" (wildcard) or when
// path.Match(expandedPattern, value) succeeds. The pattern is
// tilde-expanded before matching so "~/projects/*" works correctly.
// Malformed patterns that survived Validate are treated as non-matching.
func matchGlob(pattern, value string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	ok, _ := path.Match(ExpandPath(pattern), value)
	return ok
}

// matchKind checks the kind axis; empty pattern matches any kind.
// Valid kind values are "pending_approval" and "done".
func matchKind(pattern, value string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	return pattern == value
}

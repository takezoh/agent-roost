// Package features provides the two independent feature-flag mechanisms:
//
//   - Runtime flags: [Flag] typed constants + [Set] injected into [state.State].
//     Toggled via ~/.roost/settings.toml [features.enabled]. Both branches are
//     always compiled into the binary — equivalent to a C if(){} guard.
//
//   - Compile-time flags: top-level bool constants guarded by build tags.
//     The off-side branch is eliminated by the Go compiler's dead code
//     elimination — equivalent to a C #if/#endif guard. See build_*.go files.
//
// The two mechanisms share no key space: Flag constants are runtime flags,
// top-level bool constants (e.g. Experimental) are compile-time flags.
package features

// Flag identifies a runtime-toggled experimental feature. Using a named type
// prevents untyped string literals from leaking into call sites and makes
// misspelled identifiers a compile error.
type Flag string

const (
	// Add new runtime flags here. When a feature stabilises, delete the
	// constant and inline the enabled branch everywhere it appears.
	// Example: ExampleFeature Flag = "example-feature"

	// Peers enables agent-to-agent messaging via the roost daemon broker.
	Peers Flag = "peers"
)

// Set is the collection of enabled runtime flags. Constructed once at
// startup from the config file and injected into state.State; callers
// treat it as immutable after construction.
type Set map[Flag]bool

// On reports whether f is enabled. A nil Set returns false for every flag,
// so callers never need a nil check.
func (s Set) On(f Flag) bool {
	if s == nil {
		return false
	}
	return s[f]
}

// FromConfig builds a Set from the raw map read from the TOML config.
// Keys that are not in known are silently ignored so that removing a Flag
// constant never causes a config-parse error on existing installations.
func FromConfig(raw map[string]bool, known []Flag) Set {
	s := make(Set, len(raw))
	allowed := make(map[Flag]struct{}, len(known))
	for _, f := range known {
		allowed[f] = struct{}{}
	}
	for k, v := range raw {
		f := Flag(k)
		if _, ok := allowed[f]; !ok {
			continue
		}
		s[f] = v
	}
	return s
}

// All returns every Flag constant defined in this package. Pass the result
// as the known argument to [FromConfig] so it knows which config keys to
// accept. Add each new Flag constant here as well.
func All() []Flag {
	return []Flag{
		Peers,
	}
}

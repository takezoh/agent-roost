// Package glyphs manages the active glyph set for the TUI.
// Two built-in sets ("nerd" and "ascii") are embedded at build time.
// A user-defined partial override can be loaded from ~/.roost/glyphs.json.
// The active set is selected via ROOST_GLYPHS env var (default: "nerd").
package glyphs

var (
	activeName string
	activeSet  map[string]string
	userSet    map[string]string
)

// Use switches the active glyph set by name ("nerd" or "ascii").
// Unknown names are silently ignored (current set is preserved).
func Use(name string) {
	if _, ok := builtinSets[name]; !ok {
		return
	}
	activeName = name
	activeSet = builtinSets[name]
}

// Get returns the glyph for the given semantic key from the active set.
// User overrides take priority. Returns "?" for unknown keys.
func Get(key string) string {
	if userSet != nil {
		if g, ok := userSet[key]; ok {
			return g
		}
	}
	if activeSet != nil {
		if g, ok := activeSet[key]; ok {
			return g
		}
	}
	return "?"
}

// Active returns the name of the current glyph set.
func Active() string {
	return activeName
}

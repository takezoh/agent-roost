package glyphs

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
)

//go:embed assets/nerd.json
var nerdJSON []byte

//go:embed assets/ascii.json
var asciiJSON []byte

// builtinSets maps set name → glyph map, populated during init.
var builtinSets map[string]map[string]string

type glyphFile struct {
	Name   string            `json:"name"`
	Glyphs map[string]string `json:"glyphs"`
}

func init() {
	builtinSets = make(map[string]map[string]string)
	for _, raw := range [][]byte{nerdJSON, asciiJSON} {
		var f glyphFile
		if err := json.Unmarshal(raw, &f); err != nil {
			panic("glyphs: failed to parse embedded JSON: " + err.Error())
		}
		builtinSets[f.Name] = f.Glyphs
	}
	// Default to nerd set.
	activeName = "nerd"
	activeSet = builtinSets["nerd"]
}

// Load reads a user-defined partial glyph override from path.
// Missing file is silently ignored. Parse errors are logged and the
// built-in set continues to be used.
func Load(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var f glyphFile
	if err := json.Unmarshal(data, &f); err != nil {
		slog.Warn("glyphs: ignoring malformed user JSON", "path", path, "err", err)
		return nil
	}
	userSet = f.Glyphs
	return nil
}

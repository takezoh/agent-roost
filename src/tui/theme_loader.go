package tui

import (
	_ "embed"
	"encoding/json"
	"errors"
	"image/color"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
)

//go:embed theme_assets/default.json
var defaultThemeJSON []byte

//go:embed theme_assets/minimal.json
var minimalThemeJSON []byte

type themeFile struct {
	Name    string            `json:"name"`
	Minimal bool              `json:"minimal"`
	Colors  map[string]string `json:"colors"`
}

func init() {
	for _, raw := range [][]byte{defaultThemeJSON, minimalThemeJSON} {
		var f themeFile
		if err := json.Unmarshal(raw, &f); err != nil {
			panic("themes: bad builtin JSON: " + err.Error())
		}
		Themes[f.Name] = themeFromFile(f)
	}
	ApplyTheme("default")
}

func themeFromFile(f themeFile) Theme {
	t := newBaseTheme()
	t.Minimal = f.Minimal
	set := func(key string, dst *color.Color) {
		if v, ok := f.Colors[key]; ok && v != "" {
			*dst = lipgloss.Color(v)
		}
	}
	set("primary", &t.Primary)
	set("accent", &t.Accent)
	set("fg", &t.Fg)
	set("muted", &t.Muted)
	set("dim", &t.Dim)
	set("selBg", &t.SelBg)
	set("selFg", &t.SelFg)
	set("tagFg", &t.TagFg)
	set("running", &t.Running)
	set("waiting", &t.Waiting)
	set("idle", &t.Idle)
	set("stopped", &t.Stopped)
	set("pending", &t.Pending)
	set("warn", &t.Warn)
	set("error", &t.Error)
	set("runningGradientA", &t.RunningGradientA)
	set("runningGradientB", &t.RunningGradientB)
	return t
}

// RegisterTheme adds or replaces a named theme in the registry.
func RegisterTheme(name string, t Theme) {
	Themes[name] = t
}

// LoadThemesFromDir reads *.json files from dir and registers each as a theme.
// Missing dir is silently ignored; parse errors are logged and skipped.
func LoadThemesFromDir(dir string) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		slog.Warn("themes: cannot read dir", "dir", dir, "err", err)
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("themes: cannot read file", "path", path, "err", err)
			continue
		}
		var f themeFile
		if err := json.Unmarshal(data, &f); err != nil {
			slog.Warn("themes: ignoring malformed JSON", "path", path, "err", err)
			continue
		}
		if f.Name == "" {
			slog.Warn("themes: skipping JSON with no name field", "path", path)
			continue
		}
		Themes[f.Name] = themeFromFile(f)
	}
}

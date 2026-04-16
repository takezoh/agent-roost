package gemini

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// EnsureNoHooksSettings writes a gemini system-settings file that disables
// hooks under <dataDir>/scripts/gemini-no-hooks.json and returns a
// GEMINI_CLI_SYSTEM_SETTINGS_PATH=<path> env entry pointing to it.
// The file is written once and reused on subsequent calls.
// Returns an error only on I/O failure.
func EnsureNoHooksSettings(dataDir string) (string, error) {
	path := filepath.Join(dataDir, "scripts", "gemini-no-hooks.json")
	if _, err := os.Stat(path); err == nil {
		return "GEMINI_CLI_SYSTEM_SETTINGS_PATH=" + path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	settings := map[string]any{
		"hooksConfig": map[string]any{"enabled": false},
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return "", err
	}
	return "GEMINI_CLI_SYSTEM_SETTINGS_PATH=" + path, nil
}

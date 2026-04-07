package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const hookMarker = "roost claude event"

// RegisterHooks registers roost hooks in Claude's settings.json.
// Returns the list of registered event names.
func RegisterHooks(settingsPath, roostBinary string) ([]string, error) {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return nil, err
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = make(map[string]any)
	}

	command := roostBinary + " claude event"
	events := []string{
		"SessionStart",
		"SessionEnd",
		"PreToolUse",
		"PostToolUse",
		"PostToolUseFailure",
		"Stop",
		"StopFailure",
		"UserPromptSubmit",
		"PreCompact",
		"PostCompact",
		"Notification",
		"SubagentStart",
		"SubagentStop",
		"TaskCreated",
		"TaskCompleted",
	}
	registered := []string{}

	for _, event := range events {
		if addHookEntry(hooks, event, command) {
			registered = append(registered, event)
		}
	}

	if len(registered) == 0 {
		return nil, nil
	}

	settings["hooks"] = hooks
	return registered, writeSettings(settingsPath, settings)
}

// UnregisterHooks removes roost hooks from Claude's settings.json.
func UnregisterHooks(settingsPath string) error {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return err
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		return nil
	}

	for event, v := range hooks {
		entries, ok := v.([]any)
		if !ok {
			continue
		}
		filtered := filterNonRoost(entries)
		if len(filtered) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = filtered
		}
	}

	settings["hooks"] = hooks
	return writeSettings(settingsPath, settings)
}

// IsHookRegistered checks if roost hooks are already registered.
func IsHookRegistered(settingsPath string) (bool, error) {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return false, err
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		return false, nil
	}

	entries, _ := hooks["SessionStart"].([]any)
	for _, e := range entries {
		if isRoostEntry(e) {
			return true, nil
		}
	}
	return false, nil
}

func addHookEntry(hooks map[string]any, event, command string) bool {
	entries, _ := hooks[event].([]any)
	for _, e := range entries {
		if isRoostEntry(e) {
			return false
		}
	}

	entry := map[string]any{
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": command,
			},
		},
	}
	hooks[event] = append(entries, entry)
	return true
}

func isRoostEntry(entry any) bool {
	m, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	hooksArr, _ := m["hooks"].([]any)
	for _, h := range hooksArr {
		hm, ok := h.(map[string]any)
		if !ok {
			continue
		}
		cmd, _ := hm["command"].(string)
		if strings.Contains(cmd, hookMarker) {
			return true
		}
	}
	return false
}

func filterNonRoost(entries []any) []any {
	var result []any
	for _, e := range entries {
		if !isRoostEntry(e) {
			result = append(result, e)
		}
	}
	return result
}

func readSettings(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return make(map[string]any), nil
	}
	if err != nil {
		return nil, err
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}
	return settings, nil
}

func writeSettings(path string, settings map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

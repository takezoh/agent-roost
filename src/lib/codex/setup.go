package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type hookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

type hookEntry struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []hookCommand `json:"hooks"`
}

type hooksFile struct {
	Hooks map[string][]hookEntry `json:"hooks"`
}

type eventSpec struct {
	Name    string
	Matcher string
	Timeout int
}

var (
	reSectionHeader = regexp.MustCompile(`^\s*\[[^]]+\]\s*$`)
	reFeatures      = regexp.MustCompile(`^\s*\[features\]\s*$`)
	reCodexHooksKey = regexp.MustCompile(`^\s*codex_hooks\s*=`)
)

// RegisterMCPServer writes the roost-peers entry to ~/.codex/mcp.json.
// Returns true if the entry was newly written, false if already present.
func RegisterMCPServer(mcpPath, roostBinary string) (bool, error) {
	servers, err := readMCPServers(mcpPath)
	if err != nil {
		return false, err
	}
	if _, exists := servers["roost-peers"]; exists {
		return false, nil
	}
	servers["roost-peers"] = map[string]any{
		"command": roostBinary,
		"args":    []any{"peers-mcp"},
	}
	return true, writeMCPServers(mcpPath, servers)
}

func readMCPServers(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return make(map[string]any), nil
	}
	if err != nil {
		return nil, err
	}
	var servers map[string]any
	if err := json.Unmarshal(data, &servers); err != nil {
		return nil, err
	}
	return servers, nil
}

func writeMCPServers(path string, servers map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(servers, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func RegisterHooks(configPath, hooksPath, roostBinary string) (bool, []string, error) {
	configChanged, err := enableCodexHooks(configPath)
	if err != nil {
		return false, nil, err
	}
	command := roostBinary + " event codex"
	hooksChanged, events, err := mergeHooksFile(hooksPath, command)
	if err != nil {
		return false, nil, err
	}
	return configChanged || hooksChanged, events, nil
}

func enableCodexHooks(path string) (bool, error) {
	body, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	next, changed := ensureCodexHooksEnabled(string(body))
	if !changed {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(next), 0o644)
}

func ensureCodexHooksEnabled(in string) (string, bool) {
	lines := strings.Split(in, "\n")
	start, end := findFeaturesSection(lines)
	if start == -1 {
		return appendFeaturesSection(in), true
	}
	for i := start + 1; i < end; i++ {
		if !reCodexHooksKey.MatchString(lines[i]) {
			continue
		}
		if strings.TrimSpace(lines[i]) == "codex_hooks = true" {
			return in, false
		}
		lines[i] = "codex_hooks = true"
		return strings.Join(lines, "\n"), true
	}
	updated := insertCodexHooksLine(lines, start, end)
	return strings.Join(updated, "\n"), true
}

func appendFeaturesSection(in string) string {
	out := strings.TrimRight(in, "\n")
	if out != "" {
		out += "\n\n"
	}
	out += "[features]\n"
	out += "codex_hooks = true\n"
	return out
}

func findFeaturesSection(lines []string) (int, int) {
	start := -1
	end := len(lines)
	for i, line := range lines {
		if start == -1 && reFeatures.MatchString(line) {
			start = i
			continue
		}
		if start != -1 && reSectionHeader.MatchString(line) {
			end = i
			break
		}
	}
	return start, end
}

func insertCodexHooksLine(lines []string, start, end int) []string {
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:end]...)
	out = append(out, "codex_hooks = true")
	out = append(out, lines[end:]...)
	if start+1 == end {
		return out
	}
	return out
}

func mergeHooksFile(path, command string) (bool, []string, error) {
	hf, err := readHooks(path)
	if err != nil {
		return false, nil, err
	}
	var added []string
	for _, spec := range hookSpecs() {
		if ensureHook(hf.Hooks, spec, command) {
			added = append(added, spec.Name)
		}
	}
	if len(added) == 0 {
		return false, nil, nil
	}
	if err := writeHooks(path, hf); err != nil {
		return false, nil, err
	}
	return true, added, nil
}

func hookSpecs() []eventSpec {
	return []eventSpec{
		{Name: "SessionStart", Matcher: "startup|resume"},
		{Name: "UserPromptSubmit"},
		{Name: "Stop", Timeout: 30},
		{Name: "PreToolUse", Matcher: "Bash"},
		{Name: "PostToolUse", Matcher: "Bash"},
	}
}

func ensureHook(m map[string][]hookEntry, spec eventSpec, command string) bool {
	entries := m[spec.Name]
	for i := range entries {
		if entries[i].Matcher != spec.Matcher {
			continue
		}
		for j := range entries[i].Hooks {
			h := &entries[i].Hooks[j]
			if h.Command != command || h.Type != "command" {
				continue
			}
			changed := false
			if h.Timeout != spec.Timeout {
				h.Timeout = spec.Timeout
				changed = true
			}
			m[spec.Name] = entries
			return changed
		}
	}

	hc := hookCommand{Type: "command", Command: command}
	if spec.Timeout > 0 {
		hc.Timeout = spec.Timeout
	}
	entry := hookEntry{Hooks: []hookCommand{hc}}
	if spec.Matcher != "" {
		entry.Matcher = spec.Matcher
	}
	m[spec.Name] = append(entries, entry)
	return true
}

func readHooks(path string) (hooksFile, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return hooksFile{Hooks: map[string][]hookEntry{}}, nil
	}
	if err != nil {
		return hooksFile{}, err
	}
	var hf hooksFile
	if err := json.Unmarshal(data, &hf); err != nil {
		return hooksFile{}, err
	}
	if hf.Hooks == nil {
		hf.Hooks = map[string][]hookEntry{}
	}
	return hf, nil
}

func writeHooks(path string, hf hooksFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(hf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

package codex

import (
	"os"
	"path/filepath"
)

// EnsureNoHooksHome creates a shadow CODEX_HOME directory under
// <dataDir>/scripts/codex-no-hooks/ with hooks disabled via config.toml
// and an empty hooks.json. Returns a CODEX_HOME=<path> env entry.
// The directory is created once and reused on subsequent calls.
//
// Note: the shadow home does not include codex auth files, so callers that
// rely on ~/.codex/auth.json for authentication must provide credentials
// via environment variables (e.g. OPENAI_API_KEY) instead.
func EnsureNoHooksHome(dataDir string) (string, error) {
	shadowDir := filepath.Join(dataDir, "scripts", "codex-no-hooks")
	configPath := filepath.Join(shadowDir, "config.toml")
	if _, err := os.Stat(configPath); err == nil {
		return "CODEX_HOME=" + shadowDir, nil
	}
	if err := os.MkdirAll(shadowDir, 0o755); err != nil {
		return "", err
	}
	config := "[features]\ncodex_hooks = false\n"
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		return "", err
	}
	hooksPath := filepath.Join(shadowDir, "hooks.json")
	if err := os.WriteFile(hooksPath, []byte("{\"hooks\":{}}\n"), 0o644); err != nil {
		return "", err
	}
	return "CODEX_HOME=" + shadowDir, nil
}

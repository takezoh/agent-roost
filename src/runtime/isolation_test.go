package runtime_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoToolSpecificEnvLiterals guards against tool-specific environment variable
// names (AWS_*, ANTHROPIC_*, GOOGLE_*, OPENAI_*, etc.) appearing as string literals
// in generic layers. These names must live exclusively in auth/credproxy/<provider>/
// or lib/<tool>/ — see ARCHITECTURE.md "Driver/Connector isolation".
//
// golangci-lint forbidigo cannot detect string literals (only call expressions),
// so this test acts as the static enforcement gate.
func TestNoToolSpecificEnvLiterals(t *testing.T) {
	forbidden := []string{
		"AWS_CONTAINER_",
		"AWS_ACCESS_KEY",
		"AWS_SECRET_ACCESS",
		"AWS_SESSION_TOKEN",
		"ANTHROPIC_API_KEY",
		"ANTHROPIC_AUTH_TOKEN",
		"ANTHROPIC_BASE_URL",
		"GOOGLE_APPLICATION_CREDENTIALS",
		"OPENAI_API_KEY",
	}

	// Packages whose non-test .go files must contain no tool-specific env literals.
	srcRoot := ".."
	checkedDirs := []string{
		filepath.Join(srcRoot, "runtime"),
		filepath.Join(srcRoot, "sandbox"),
		filepath.Join(srcRoot, "state"),
		filepath.Join(srcRoot, "tui"),
		filepath.Join(srcRoot, "proto"),
	}

	for _, dir := range checkedDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			// Directory may not exist for all builds; skip gracefully.
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("read %s: %v", path, err)
				continue
			}
			for _, kw := range forbidden {
				if bytes.Contains(data, []byte(`"`+kw)) {
					t.Errorf(
						"%s contains tool-specific env literal %q\n"+
							"  → move to auth/credproxy/<provider>/ or lib/<tool>/ (see ARCHITECTURE.md)",
						path, kw,
					)
				}
			}
		}
	}
}

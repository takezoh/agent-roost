package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedirectStderr(t *testing.T) {
	dir := t.TempDir()
	if err := InitWithDataDir("info", dir); err != nil {
		t.Fatal(err)
	}
	defer Close()

	RedirectStderr()

	marker := "REDIRECT_TEST_MARKER_12345"
	fmt.Fprintln(os.Stderr, marker)

	data, err := os.ReadFile(filepath.Join(dir, "roost.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), marker) {
		t.Errorf("marker not found in log file after RedirectStderr")
	}
}

func TestRedirectStderrNilLogFile(t *testing.T) {
	saved := logFile
	logFile = nil
	defer func() { logFile = saved }()

	// Must not panic.
	RedirectStderr()
}

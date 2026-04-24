package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveSocketPath_envOverride(t *testing.T) {
	t.Setenv("ROOST_SOCKET", "/tmp/custom.sock")
	got, err := resolveSocketPath()
	if err != nil {
		t.Fatalf("resolveSocketPath: %v", err)
	}
	if got != "/tmp/custom.sock" {
		t.Errorf("got %q, want /tmp/custom.sock (ROOST_SOCKET must win)", got)
	}
}

func TestResolveSocketPath_fallback(t *testing.T) {
	t.Setenv("ROOST_SOCKET", "")
	got, err := resolveSocketPath()
	if err != nil {
		t.Fatalf("resolveSocketPath: %v", err)
	}
	if !strings.HasSuffix(got, "roost.sock") {
		t.Errorf("got %q, want suffix \"roost.sock\" (fallback path must end in roost.sock)", got)
	}
	if filepath.IsAbs(got) == false {
		t.Errorf("fallback path %q should be absolute", got)
	}
}

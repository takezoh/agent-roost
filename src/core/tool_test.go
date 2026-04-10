package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMakeProjectDir(t *testing.T) {
	root := t.TempDir()
	roots := []string{root}

	t.Run("creates directory", func(t *testing.T) {
		path, err := makeProjectDir(roots, root, "foo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(root, "foo")
		if path != want {
			t.Errorf("path = %q, want %q", path, want)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat created dir: %v", err)
		}
		if !info.IsDir() {
			t.Error("created path is not a directory")
		}
	})

	t.Run("rejects existing directory", func(t *testing.T) {
		dst := filepath.Join(root, "exists")
		if err := os.Mkdir(dst, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := makeProjectDir(roots, root, "exists"); err == nil {
			t.Error("expected error for existing directory, got nil")
		}
	})

	cases := []struct {
		name    string
		root    string
		project string
		errSnip string
	}{
		{"unknown root", "/tmp/not-in-config", "foo", "configured project_roots"},
		{"empty root", "", "foo", "configured project_roots"},
		{"empty name", root, "", "name required"},
		{"path traversal dotdot", root, "..", "invalid project name"},
		{"nested path slash", root, "a/b", "invalid project name"},
		{"nested path backslash", root, `a\b`, "invalid project name"},
		{"hidden", root, ".hidden", "invalid project name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := makeProjectDir(roots, tc.root, tc.project)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.errSnip)
			}
			if !strings.Contains(err.Error(), tc.errSnip) {
				t.Errorf("err = %v, want substring %q", err, tc.errSnip)
			}
		})
	}

	// Verify validation rejects names before Mkdir touches FS.
	for _, name := range []string{"a/b", `a\b`, ".hidden"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			t.Errorf("validation case %q created a path on disk", name)
		}
	}
}

func TestRunCreateProject(t *testing.T) {
	root := t.TempDir()
	ctx := &ToolContext{
		Config: ToolConfig{ProjectRoots: []string{root}},
	}

	t.Run("returns new-session invocation on success", func(t *testing.T) {
		next, err := runCreateProject(ctx, map[string]string{"root": root, "name": "alpha"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if next == nil {
			t.Fatal("expected non-nil ToolInvocation, got nil")
		}
		if next.Name != "new-session" {
			t.Errorf("next.Name = %q, want %q", next.Name, "new-session")
		}
		wantPath := filepath.Join(root, "alpha")
		if got := next.Args["project"]; got != wantPath {
			t.Errorf("next.Args[project] = %q, want %q", got, wantPath)
		}
		if _, err := os.Stat(wantPath); err != nil {
			t.Errorf("expected project dir to exist: %v", err)
		}
	})

	t.Run("returns nil invocation on failure", func(t *testing.T) {
		next, err := runCreateProject(ctx, map[string]string{"root": "/not/configured", "name": "beta"})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if next != nil {
			t.Errorf("expected nil ToolInvocation on error, got %+v", next)
		}
	})
}


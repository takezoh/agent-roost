package proto

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalProjectPath_trailingSlash(t *testing.T) {
	dir := t.TempDir()
	withSlash := dir + "/"
	got := canonicalProjectPath(withSlash)
	if got != dir {
		t.Errorf("canonicalProjectPath(%q) = %q, want %q", withSlash, got, dir)
	}
}

func TestCanonicalProjectPath_relative(t *testing.T) {
	got := canonicalProjectPath(".")
	if !filepath.IsAbs(got) {
		t.Errorf("canonicalProjectPath(\".\") returned relative path: %q", got)
	}
}

func TestCanonicalProjectPath_symlink(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skip("symlink creation failed:", err)
	}
	got := canonicalProjectPath(link)
	// Symlink must be resolved; resolved path equals the real dir.
	if got == link {
		t.Errorf("symlink not resolved: canonicalProjectPath(%q) = %q", link, got)
	}
	if got != dir {
		t.Errorf("canonicalProjectPath(%q) = %q, want %q", link, got, dir)
	}
}

func TestCanonicalProjectPath_nonexistent(t *testing.T) {
	p := "/nonexistent/path/xyz123abc"
	got := canonicalProjectPath(p)
	if got != p {
		t.Errorf("nonexistent path changed unexpectedly: got %q, want %q", got, p)
	}
}

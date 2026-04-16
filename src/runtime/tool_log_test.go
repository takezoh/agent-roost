package runtime

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"
)

func TestFileToolLog_AppendRoundtrip(t *testing.T) {
	dir := t.TempDir()
	tl := NewFileToolLog(dir)

	if err := tl.Append("claude", "-my-project", `{"kind":"auto"}`); err != nil {
		t.Fatalf("first Append: %v", err)
	}
	if err := tl.Append("claude", "-my-project", `{"kind":"approved"}`); err != nil {
		t.Fatalf("second Append: %v", err)
	}

	path := filepath.Join(dir, "claude", "tool-logs", "-my-project.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open log file: %v", err)
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(lines))
	}
	if lines[0] != `{"kind":"auto"}` {
		t.Errorf("line[0] = %q", lines[0])
	}
	if lines[1] != `{"kind":"approved"}` {
		t.Errorf("line[1] = %q", lines[1])
	}
}

func TestFileToolLog_MultipleProjects(t *testing.T) {
	dir := t.TempDir()
	tl := NewFileToolLog(dir)

	if err := tl.Append("claude", "-project-a", `{"kind":"auto"}`); err != nil {
		t.Fatalf("Append A: %v", err)
	}
	if err := tl.Append("claude", "-project-b", `{"kind":"approved"}`); err != nil {
		t.Fatalf("Append B: %v", err)
	}

	logDir := filepath.Join(dir, "claude", "tool-logs")
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("want 2 files, got %d", len(entries))
	}
}

func TestFileToolLog_MultipleNamespaces(t *testing.T) {
	dir := t.TempDir()
	tl := NewFileToolLog(dir)

	if err := tl.Append("claude", "-proj", `{"kind":"auto"}`); err != nil {
		t.Fatalf("Append claude: %v", err)
	}
	if err := tl.Append("codex", "-proj", `{"kind":"auto"}`); err != nil {
		t.Fatalf("Append codex: %v", err)
	}

	claudePath := filepath.Join(dir, "claude", "tool-logs", "-proj.jsonl")
	codexPath := filepath.Join(dir, "codex", "tool-logs", "-proj.jsonl")
	if _, err := os.Stat(claudePath); os.IsNotExist(err) {
		t.Error("claude namespace file not created")
	}
	if _, err := os.Stat(codexPath); os.IsNotExist(err) {
		t.Error("codex namespace file not created")
	}
}

func TestFileToolLog_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	tl := NewFileToolLog(dir)

	// Directory does not exist yet — Append must create it.
	if err := tl.Append("claude", "-project", `{"kind":"auto"}`); err != nil {
		t.Fatalf("Append: %v", err)
	}
	logDir := filepath.Join(dir, "claude", "tool-logs")
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		t.Error("log directory was not created")
	}
}

func TestFileToolLog_RejectsBadSlug(t *testing.T) {
	dir := t.TempDir()
	tl := NewFileToolLog(dir)

	cases := []struct {
		name string
		slug string
	}{
		{"empty", ""},
		{"absolute", "/abs/path"},
		{"contains slash", "a/b"},
		{"dotdot", "../etc"},
		{"dotdot embedded", "a..b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// bad project slug with valid namespace
			if err := tl.Append("claude", tc.slug, "{}"); err == nil {
				t.Errorf("Append(\"claude\", %q) should have failed", tc.slug)
			}
		})
	}
}

func TestFileToolLog_RejectsBadNamespace(t *testing.T) {
	dir := t.TempDir()
	tl := NewFileToolLog(dir)

	cases := []struct {
		name string
		ns   string
	}{
		{"empty", ""},
		{"absolute", "/abs/path"},
		{"contains slash", "a/b"},
		{"dotdot", "../etc"},
		{"dotdot embedded", "a..b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tl.Append(tc.ns, "-project", "{}"); err == nil {
				t.Errorf("Append(%q, \"-project\") should have failed", tc.ns)
			}
		})
	}
}

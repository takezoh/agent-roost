package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileToolLog is the production ToolLogBackend. It writes one JSONL
// file per project under <dataDir>/<namespace>/tool-logs/<slug>.jsonl.
//
// Namespace is an opaque driver-supplied token (e.g. "claude"). The
// runtime never interprets its value — it is simply used as a path
// component, keeping driver names out of the runtime package.
//
// Each Append call opens the file, writes the line, and closes the
// file to avoid accumulating file descriptors across an unbounded
// number of project directories. Tool-use events arrive on the order
// of seconds, so the open/close overhead is negligible.
type FileToolLog struct {
	dir string // <dataDir>
}

// NewFileToolLog returns a FileToolLog rooted at dataDir.
// Subdirectories are created lazily on the first successful append.
func NewFileToolLog(dataDir string) *FileToolLog {
	return &FileToolLog{dir: dataDir}
}

// Append writes a single JSONL line to the project's log file at
// <dataDir>/<namespace>/tool-logs/<project>.jsonl.
// A trailing newline is added automatically.
//
// Both namespace and project must be non-empty slugs: no absolute
// paths, no "..", no "/" separators.
func (f *FileToolLog) Append(namespace, project, line string) error {
	if err := validateSlug(namespace); err != nil {
		return fmt.Errorf("tool_log: namespace: %w", err)
	}
	if err := validateSlug(project); err != nil {
		return fmt.Errorf("tool_log: project: %w", err)
	}
	dir := filepath.Join(f.dir, namespace, "tool-logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("tool_log: mkdir: %w", err)
	}
	path := filepath.Join(dir, project+".jsonl")
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("tool_log: open: %w", err)
	}
	defer fh.Close()
	if _, err := fmt.Fprintln(fh, line); err != nil {
		return fmt.Errorf("tool_log: write: %w", err)
	}
	return nil
}

// validateSlug rejects slugs that could escape the log directory or
// produce ambiguous paths.
func validateSlug(slug string) error {
	switch {
	case slug == "":
		return fmt.Errorf("invalid slug: empty")
	case filepath.IsAbs(slug):
		return fmt.Errorf("invalid slug: absolute path %q", slug)
	case strings.Contains(slug, "/"):
		return fmt.Errorf("invalid slug: contains slash %q", slug)
	case strings.Contains(slug, ".."):
		return fmt.Errorf("invalid slug: contains '..' %q", slug)
	}
	return nil
}

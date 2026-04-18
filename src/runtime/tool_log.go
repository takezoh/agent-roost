package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// FileToolLog is the production ToolLogBackend. It writes one JSONL
// file per project under <dataDir>/<namespace>/tool-logs/<slug>.jsonl.
//
// Files are kept open across calls (lazy-open on first append, keyed by
// "namespace/project") and closed together on CloseAll. This avoids the
// per-call open/write/close syscall overhead while bounding fd growth to
// the set of distinct (namespace, project) pairs actually written.
type FileToolLog struct {
	dir   string
	mu    sync.Mutex
	files map[string]*os.File
}

// NewFileToolLog returns a FileToolLog rooted at dataDir.
// Subdirectories are created lazily on the first successful append.
func NewFileToolLog(dataDir string) *FileToolLog {
	return &FileToolLog{dir: dataDir, files: make(map[string]*os.File)}
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

	f.mu.Lock()
	defer f.mu.Unlock()

	key := namespace + "/" + project
	fh, ok := f.files[key]
	if !ok {
		dir := filepath.Join(f.dir, namespace, "tool-logs")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("tool_log: mkdir: %w", err)
		}
		path := filepath.Join(dir, project+".jsonl")
		var err error
		fh, err = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("tool_log: open: %w", err)
		}
		f.files[key] = fh
	}

	if _, err := fmt.Fprintln(fh, line); err != nil {
		return fmt.Errorf("tool_log: write: %w", err)
	}
	return nil
}

// CloseAll closes all open log file descriptors. Called on daemon shutdown.
func (f *FileToolLog) CloseAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for key, fh := range f.files {
		fh.Close()
		delete(f.files, key)
	}
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

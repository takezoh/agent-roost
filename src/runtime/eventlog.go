package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/take/agent-roost/state"
)

// FileEventLog is the production EventLogBackend. It writes one file
// per session under <dataDir>/events/<sessionID>.log, opening lazily
// on first append and keeping the file handle open for the rest of
// the session's lifetime. Close(sessionID) is called from the runtime
// when a session is destroyed; CloseAll runs at shutdown.
//
// The implementation is single-writer per session — the runtime
// dispatches all EffEventLogAppend effects from the event loop
// goroutine, so the open() / write() pair never races. The internal
// mu just protects the files map (Append / Close may be called
// concurrently with CloseAll during shutdown).
type FileEventLog struct {
	dir string

	mu    sync.Mutex
	files map[state.SessionID]*os.File
}

// NewFileEventLog returns a FileEventLog rooted at <dataDir>/events.
// The caller must ensure the parent dir exists; the implementation
// MkdirAlls the events subdirectory itself on first append.
func NewFileEventLog(dataDir string) *FileEventLog {
	return &FileEventLog{
		dir:   filepath.Join(dataDir, "events"),
		files: map[state.SessionID]*os.File{},
	}
}

// Append writes a single line to the session's event log file,
// prefixed with an RFC3339 timestamp. Lazy-opens the file on first
// call.
func (f *FileEventLog) Append(sessionID state.SessionID, line string) error {
	if strings.ContainsAny(string(sessionID), `/\.`) {
		return fmt.Errorf("eventlog: invalid session id: %q", sessionID)
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	fh, ok := f.files[sessionID]
	if !ok {
		if err := os.MkdirAll(f.dir, 0o755); err != nil {
			return fmt.Errorf("eventlog: mkdir: %w", err)
		}
		path := filepath.Join(f.dir, string(sessionID)+".log")
		var err error
		fh, err = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("eventlog: open: %w", err)
		}
		f.files[sessionID] = fh
	}

	stamp := time.Now().UTC().Format(time.RFC3339)
	if _, err := fmt.Fprintf(fh, "%s %s\n", stamp, line); err != nil {
		return fmt.Errorf("eventlog: write: %w", err)
	}
	return nil
}

// Close closes the file for one session and removes it from the map.
// Subsequent Append calls reopen the file.
func (f *FileEventLog) Close(sessionID state.SessionID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if fh, ok := f.files[sessionID]; ok {
		fh.Close()
		delete(f.files, sessionID)
	}
}

// CloseAll shuts every open file. Called from runtime.Stop on
// shutdown.
func (f *FileEventLog) CloseAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, fh := range f.files {
		fh.Close()
		delete(f.files, id)
	}
}

// Path returns the on-disk path of a session's log file.
func (f *FileEventLog) Path(sessionID state.SessionID) string {
	return filepath.Join(f.dir, string(sessionID)+".log")
}

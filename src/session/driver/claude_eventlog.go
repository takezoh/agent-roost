package driver

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// appendEventLog writes a single timestamped line to this driver's
// per-session event log file. Lazy file open: the file is created the
// first time it's written so sessions that never receive hook events
// don't leave empty files behind.
//
// The driver owns the file handle for its entire lifetime; Close
// flushes and releases it. Failures are logged but never propagated —
// event logging is best-effort and must not block driver state updates.
func (d *claudeDriver) appendEventLog(line string) {
	if d.eventLogDir == "" || d.sessionID == "" {
		return
	}
	d.eventLogMu.Lock()
	defer d.eventLogMu.Unlock()
	if d.eventLogF == nil {
		path := filepath.Join(d.eventLogDir, d.sessionID+".log")
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			slog.Warn("claude driver: open event log failed", "path", path, "err", err)
			return
		}
		d.eventLogF = f
	}
	if _, err := fmt.Fprintf(d.eventLogF, "%s %s\n", time.Now().Format("15:04:05"), line); err != nil {
		slog.Warn("claude driver: write event log failed", "err", err)
	}
}

// closeEventLog flushes and closes the event log file. Idempotent.
// Called from claudeDriver.Close().
func (d *claudeDriver) closeEventLog() {
	d.eventLogMu.Lock()
	defer d.eventLogMu.Unlock()
	if d.eventLogF == nil {
		return
	}
	if err := d.eventLogF.Close(); err != nil {
		slog.Warn("claude driver: close event log failed", "err", err)
	}
	d.eventLogF = nil
}

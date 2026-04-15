// Package logger initializes the global slog handler. Init or
// InitWithDataDir must be called exactly once at program startup
// before any goroutines are spawned — the package globals are not
// synchronized for concurrent access.
package logger

import (
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

var (
	logFile *os.File
	logPath string
)

// LogFilePath returns the on-disk path of the global daemon log file.
// After Init(level) or InitWithDataDir(level, dataDir) has been called,
// this returns the resolved path. Before Init it returns the default
// (~/.roost/roost.log).
func LogFilePath() string {
	if logPath != "" {
		return logPath
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".roost", "roost.log")
}

// Init opens the log file under the default data directory
// (~/.roost/) and installs a slog text handler at the given level.
func Init(level string) error {
	return InitWithDataDir(level, "")
}

// Rotate shifts existing log files under dir at process startup:
// roost.log → roost.log.1, …, up to maxRotations. Must be called
// before InitWithDataDir so that the file handle opened by Init always
// points at the freshly-created roost.log inode. Only the coordinator
// process should call this; subprocess calls to InitWithDataDir append
// to the coordinator's log file without rotating.
func Rotate(dir string) {
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".roost")
	}
	rotateLogs(filepath.Join(dir, "roost.log"))
}

// InitWithDataDir opens the log file under the given data directory
// (or the default if dataDir is empty) and installs a slog text handler.
// The resolved path is returned by LogFilePath() after this call.
// Call Rotate(dir) first if log rotation is desired (coordinator only).
func InitWithDataDir(level, dir string) error {
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".roost")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	logPath = filepath.Join(dir, "roost.log")

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	logFile = f
	opts := &slog.HandlerOptions{Level: parseLevel(level)}
	slog.SetDefault(slog.New(slog.NewTextHandler(logFile, opts)))
	return nil
}


const maxRotations = 5

// rotateLogs shifts existing log files at startup:
// roost.log → roost.log.1, roost.log.1 → roost.log.2, … up to maxRotations.
// Errors are silently ignored; missing files are not an error.
func rotateLogs(logPath string) {
	os.Remove(fmt.Sprintf("%s.%d", logPath, maxRotations))
	for i := maxRotations - 1; i >= 1; i-- {
		os.Rename(fmt.Sprintf("%s.%d", logPath, i), fmt.Sprintf("%s.%d", logPath, i+1))
	}
	os.Rename(logPath, logPath+".1")
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func Close() {
	if logFile != nil {
		logFile.Close()
	}
}

// RedirectStderr redirects OS file descriptor 2 (stderr) to the log
// file so that unexpected writes (panics, library output) do not
// corrupt the Bubble Tea TUI rendering. Also redirects Go's standard
// log package. Must be called after Init/InitWithDataDir.
func RedirectStderr() {
	if logFile == nil {
		return
	}
	if err := syscall.Dup2(int(logFile.Fd()), 2); err != nil {
		return
	}
	os.Stderr = logFile
	log.SetOutput(logFile)
}

// Package logger initializes the global slog handler. Init or
// InitWithDataDir must be called exactly once at program startup
// before any goroutines are spawned — the package globals are not
// synchronized for concurrent access.
package logger

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

// InitWithDataDir opens the log file under the given data directory
// (or the default if dataDir is empty) and installs a slog text handler.
// The resolved path is returned by LogFilePath() after this call.
func InitWithDataDir(level, dir string) error {
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".roost")
	}
	logPath = filepath.Join(dir, "roost.log")
	os.MkdirAll(dir, 0o755)

	var err error
	logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	opts := &slog.HandlerOptions{Level: parseLevel(level)}
	slog.SetDefault(slog.New(slog.NewTextHandler(logFile, opts)))
	return nil
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

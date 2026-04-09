package logger

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

var logFile *os.File

func LogFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "roost", "roost.log")
}

// Init opens the log file and installs a slog text handler at the given level.
// level is matched case-insensitively against "debug" / "info" / "warn" /
// "error"; anything else (including "") falls back to info. Init is safe to
// call from any subcommand entry point — every short-lived process (e.g. the
// hook bridge `roost claude event`) needs to share the same configured level
// so its slog.Debug calls actually reach the log file.
func Init(level string) error {
	p := LogFilePath()
	os.MkdirAll(filepath.Dir(p), 0o755)

	var err error
	logFile, err = os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
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

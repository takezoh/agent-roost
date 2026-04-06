package logger

import (
	"log/slog"
	"os"
	"path/filepath"
)

var logFile *os.File

func Init() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".config", "roost")
	os.MkdirAll(dir, 0o755)

	logFile, err = os.OpenFile(filepath.Join(dir, "roost.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(logFile, nil)))
	return nil
}

func Close() {
	if logFile != nil {
		logFile.Close()
	}
}

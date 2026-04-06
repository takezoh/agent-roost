package logger

import (
	"log/slog"
	"os"
	"path/filepath"
)

var logFile *os.File

func LogFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "roost", "roost.log")
}

func Init() error {
	p := LogFilePath()
	os.MkdirAll(filepath.Dir(p), 0o755)

	var err error
	logFile, err = os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
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

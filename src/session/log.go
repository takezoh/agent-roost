package session

import (
	"fmt"
	"os"
	"path/filepath"
)

func LogDirPath(dataDir string) string {
	return filepath.Join(dataDir, "logs")
}

func EnsureLogDir(dataDir string) string {
	dir := LogDirPath(dataDir)
	os.MkdirAll(dir, 0o755)
	return dir
}

func LogPath(dataDir, sessionID string) string {
	return filepath.Join(LogDirPath(dataDir), sessionID+".log")
}

func TailCommand(dataDir, sessionID string) string {
	return fmt.Sprintf("tail -f %s", LogPath(dataDir, sessionID))
}

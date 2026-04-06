package session

import (
	"fmt"
	"os"
	"path/filepath"
)

func LogDir(dataDir string) string {
	dir := filepath.Join(dataDir, "logs")
	os.MkdirAll(dir, 0o755)
	return dir
}

func LogPath(dataDir, sessionID string) string {
	return filepath.Join(LogDir(dataDir), sessionID+".log")
}

func TailCommand(dataDir, sessionID string) string {
	return fmt.Sprintf("tail -f %s", LogPath(dataDir, sessionID))
}

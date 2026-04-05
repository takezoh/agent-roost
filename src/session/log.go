package session

import (
	"fmt"
	"os"
	"path/filepath"
)

func LogDir() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".config", "roost", "logs")
	os.MkdirAll(dir, 0o755)
	return dir
}

func LogPath(sessionID string) string {
	return filepath.Join(LogDir(), sessionID+".log")
}

func TailCommand(sessionID string) string {
	return fmt.Sprintf("tail -f %s", LogPath(sessionID))
}

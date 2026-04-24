package docker

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// defaultImage is used when neither the user config nor .devcontainer/devcontainer.json
// specifies an image. Node-based because it is the most common runtime for
// CLI-driven agent tooling; override with a project-specific image when needed.
const defaultImage = "node:22-bookworm-slim"

// devcontainerJSON is the minimal subset of .devcontainer/devcontainer.json
// that we parse to find the image name. Only the "image" field is read;
// full devcontainer spec parsing is out of scope for P2.1.
type devcontainerJSON struct {
	Image string `json:"image"`
}

// ResolveImage returns the Docker image to use for projectPath.
// Priority:
//  1. userImage from config (if non-empty)
//  2. .devcontainer/devcontainer.json "image" field (if present)
//  3. defaultImage
func ResolveImage(projectPath, userImage string) string {
	if userImage != "" {
		return userImage
	}
	if img := readDevcontainerImage(projectPath); img != "" {
		return img
	}
	return defaultImage
}

func resolveImage(projectPath, userImage string) string { return ResolveImage(projectPath, userImage) }

func readDevcontainerImage(projectPath string) string {
	path := filepath.Join(projectPath, ".devcontainer", "devcontainer.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var dc devcontainerJSON
	if err := json.Unmarshal(data, &dc); err != nil {
		return ""
	}
	return dc.Image
}

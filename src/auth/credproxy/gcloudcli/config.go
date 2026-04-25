// Package gcloudcli provides credential isolation for the gcloud CLI in Docker containers.
//
// Instead of bind-mounting ~/.config/gcloud (which exposes OAuth refresh tokens), roost
// writes a synthetic CLOUDSDK_CONFIG directory containing per-project gcloud configurations.
// Each configuration uses auth/access_token_file pointing to a host-refreshed token file.
// Containers receive only short-lived access tokens (≤1h TTL).
package gcloudcli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ConfigDirEnv is the gcloud SDK environment variable that overrides ~/.config/gcloud.
// Setting this in container env redirects gcloud to the synthetic config dir.
const ConfigDirEnv = "CLOUDSDK_CONFIG"

// containerTokenPath is where the token file is mounted inside the container.
const containerTokenPath = "/opt/roost/gcp-token"

// containerConfigPath is where the synthetic CLOUDSDK_CONFIG dir is mounted.
const containerConfigPath = "/opt/roost/gcloud-config"

// WriteConfigDir materializes a synthetic CLOUDSDK_CONFIG directory at dir.
// One gcloud configuration named after each project ID is written; the first
// project becomes the active configuration.
// Each configuration sets auth/access_token_file to containerTokenPath.
func WriteConfigDir(dir, account string, projects []string) error {
	configsDir := filepath.Join(dir, "configurations")
	if err := os.MkdirAll(configsDir, 0o755); err != nil {
		return fmt.Errorf("gcloudcli: mkdir configurations: %w", err)
	}

	for _, proj := range projects {
		if err := validateProjectID(proj); err != nil {
			return fmt.Errorf("gcloudcli: invalid project id %q: %w", proj, err)
		}
		path := filepath.Join(configsDir, "config_"+proj)
		if err := writeConfigFile(path, account, proj); err != nil {
			return err
		}
	}

	// active_config holds just the name of the active configuration (no "config_" prefix).
	activeConfig := filepath.Join(dir, "active_config")
	if err := os.WriteFile(activeConfig, []byte(projects[0]), 0o644); err != nil {
		return fmt.Errorf("gcloudcli: write active_config: %w", err)
	}
	return nil
}

func writeConfigFile(path, account, project string) error {
	var sb strings.Builder
	sb.WriteString("[auth]\n")
	sb.WriteString("access_token_file = ")
	sb.WriteString(containerTokenPath)
	sb.WriteString("\n\n[core]\n")
	if account != "" {
		sb.WriteString("account = ")
		sb.WriteString(account)
		sb.WriteString("\n")
	}
	if project != "" {
		sb.WriteString("project = ")
		sb.WriteString(project)
		sb.WriteString("\n")
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// RenderConfig writes the gcloud configuration ini file to w (for testing).
func RenderConfig(w io.Writer, account, project string) error {
	_, err := fmt.Fprintf(w, "[auth]\naccess_token_file = %s\n\n[core]\naccount = %s\nproject = %s\n",
		containerTokenPath, account, project)
	return err
}

// ContainerEnv returns the env vars to inject into the container.
// configHostDir is the host-side path of the synthetic CLOUDSDK_CONFIG dir.
func ContainerEnv() map[string]string {
	return map[string]string{
		ConfigDirEnv: containerConfigPath,
	}
}

// ContainerMounts returns the bind-mount specs for gcloud isolation.
// tokenHostPath is the host path of the refreshed access token file.
// configHostDir is the host path of the synthetic CLOUDSDK_CONFIG dir.
func ContainerMounts(tokenHostPath, configHostDir string) []string {
	return []string{
		tokenHostPath + ":" + containerTokenPath + ":ro",
		configHostDir + ":" + containerConfigPath + ":ro",
	}
}

func validateProjectID(id string) error {
	if id == "" {
		return fmt.Errorf("empty project id")
	}
	for _, c := range id {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '_' && c != ':' {
			return fmt.Errorf("invalid character %q (project ids contain lowercase letters, digits, hyphens, underscores)", c)
		}
	}
	return nil
}

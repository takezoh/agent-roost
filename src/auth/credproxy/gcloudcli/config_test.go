package gcloudcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteConfigDir_createsFilesPerProject(t *testing.T) {
	dir := t.TempDir()
	err := WriteConfigDir(dir, "user@example.com", []string{"proj-a", "proj-b"})
	if err != nil {
		t.Fatalf("WriteConfigDir: %v", err)
	}

	// Both configuration files should exist.
	for _, proj := range []string{"proj-a", "proj-b"} {
		path := filepath.Join(dir, "configurations", "config_"+proj)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		content := string(data)
		if !strings.Contains(content, containerTokenPath) {
			t.Errorf("%s: missing access_token_file line", proj)
		}
		if !strings.Contains(content, "user@example.com") {
			t.Errorf("%s: missing account line", proj)
		}
		if !strings.Contains(content, proj) {
			t.Errorf("%s: missing project line", proj)
		}
	}

	// active_config should name the first project.
	active, err := os.ReadFile(filepath.Join(dir, "active_config"))
	if err != nil {
		t.Fatalf("read active_config: %v", err)
	}
	if string(active) != "proj-a" {
		t.Errorf("active_config = %q, want %q", string(active), "proj-a")
	}
}

func TestWriteConfigDir_rejectsInvalidProjectID(t *testing.T) {
	dir := t.TempDir()
	err := WriteConfigDir(dir, "user@example.com", []string{"bad project!"})
	if err == nil {
		t.Fatal("expected error for invalid project id")
	}
}

func TestWriteConfigDir_rejectsEmptyProjectID(t *testing.T) {
	dir := t.TempDir()
	err := WriteConfigDir(dir, "user@example.com", []string{""})
	if err == nil {
		t.Fatal("expected error for empty project id")
	}
}

func TestContainerEnv_containsCloudsdkConfig(t *testing.T) {
	env := ContainerEnv("")
	if env[ConfigDirEnv] != containerConfigPath {
		t.Errorf("ContainerEnv()[%q] = %q, want %q", ConfigDirEnv, env[ConfigDirEnv], containerConfigPath)
	}
}

func TestContainerEnv_withToken_injectsOAuthVar(t *testing.T) {
	env := ContainerEnv("ya29.test-token")
	if env[OAuthAccessTokenEnv] != "ya29.test-token" {
		t.Errorf("ContainerEnv()[%q] = %q, want %q", OAuthAccessTokenEnv, env[OAuthAccessTokenEnv], "ya29.test-token")
	}
}

func TestContainerEnv_emptyToken_omitsOAuthVar(t *testing.T) {
	env := ContainerEnv("")
	if _, ok := env[OAuthAccessTokenEnv]; ok {
		t.Errorf("ContainerEnv() should not set %s when token is empty", OAuthAccessTokenEnv)
	}
}

func TestContainerMounts_format(t *testing.T) {
	mounts := ContainerMounts("/host/token", "/host/config")
	if len(mounts) != 2 {
		t.Fatalf("want 2 mounts, got %d", len(mounts))
	}
	wantToken := "/host/token:" + containerTokenPath + ":ro"
	wantConfig := "/host/config:" + containerConfigPath + ":rw"
	if mounts[0] != wantToken {
		t.Errorf("mount[0] = %q, want %q", mounts[0], wantToken)
	}
	if mounts[1] != wantConfig {
		t.Errorf("mount[1] = %q, want %q", mounts[1], wantConfig)
	}
}

package gcloudcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takezoh/agent-roost/config"
)

func TestSpecBuilder_emptyAccount_zeroSpec(t *testing.T) {
	b := NewSpecBuilder(context.Background(), t.TempDir())
	spec, err := b.ContainerSpec(context.Background(), "/proj", config.SandboxConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spec.Env) != 0 || len(spec.Mounts) != 0 {
		t.Errorf("expected zero spec, got env=%v mounts=%v", spec.Env, spec.Mounts)
	}
}

func TestSpecBuilder_emptyProjects_zeroSpec(t *testing.T) {
	b := NewSpecBuilder(context.Background(), t.TempDir())
	sb := config.SandboxConfig{
		Proxy: config.ProxyConfig{GCP: config.GCPConfig{Account: "user@example.com"}},
	}
	spec, err := b.ContainerSpec(context.Background(), "/proj", sb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spec.Env) != 0 || len(spec.Mounts) != 0 {
		t.Errorf("expected zero spec, got env=%v mounts=%v", spec.Env, spec.Mounts)
	}
}

func TestSpecBuilder_withConfig_injectsEnvAndMounts(t *testing.T) {
	stubGcloudForSpec(t, "gcp-test-token")

	gcpDir := t.TempDir()
	b := NewSpecBuilder(context.Background(), gcpDir)

	sb := config.SandboxConfig{
		Proxy: config.ProxyConfig{
			GCP: config.GCPConfig{
				Account:  "user@example.com",
				Projects: []string{"proj-a", "proj-b"},
			},
		},
	}

	spec, err := b.ContainerSpec(context.Background(), "/myproject", sb)
	if err != nil {
		t.Fatalf("ContainerSpec: %v", err)
	}

	if spec.Env[ConfigDirEnv] != containerConfigPath {
		t.Errorf("env[%s] = %q, want %q", ConfigDirEnv, spec.Env[ConfigDirEnv], containerConfigPath)
	}
	if len(spec.Mounts) != 2 {
		t.Errorf("expected 2 mounts, got %d: %v", len(spec.Mounts), spec.Mounts)
	}

	// Verify token file was written.
	found := false
	for _, m := range spec.Mounts {
		if strings.Contains(m, "access-token") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected mount with access-token, got: %v", spec.Mounts)
	}

	// Verify gcloud-config dir was written.
	foundConfig := false
	for _, m := range spec.Mounts {
		if strings.Contains(m, "gcloud-config") {
			foundConfig = true
		}
	}
	if !foundConfig {
		t.Errorf("expected mount with gcloud-config, got: %v", spec.Mounts)
	}
}

func TestSpecBuilder_refresherDeduplication(t *testing.T) {
	stubGcloudForSpec(t, "tok")

	b := NewSpecBuilder(context.Background(), t.TempDir())
	sb := config.SandboxConfig{
		Proxy: config.ProxyConfig{
			GCP: config.GCPConfig{Account: "user@example.com", Projects: []string{"p"}},
		},
	}

	// Call twice — refresher goroutine should only start once.
	if _, err := b.ContainerSpec(context.Background(), "/p1", sb); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ContainerSpec(context.Background(), "/p2", sb); err != nil {
		t.Fatal(err)
	}

	b.mu.Lock()
	count := len(b.refreshers)
	b.mu.Unlock()

	if count != 1 {
		t.Errorf("expected 1 refresher, got %d", count)
	}
}

// stubGcloudForSpec writes a fake gcloud script and prepends its dir to PATH.
func stubGcloudForSpec(t *testing.T, token string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "gcloud")
	content := "#!/bin/sh\necho " + token + "\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write stub gcloud: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

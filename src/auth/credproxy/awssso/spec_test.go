package awssso

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/takezoh/agent-roost/config"
)

func TestSpecBuilder_emptyProfiles_zeroSpec(t *testing.T) {
	b := NewSpecBuilder("127.0.0.1:9100", "tok", t.TempDir())
	spec, err := b.ContainerSpec(context.Background(), "/proj", config.SandboxConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(spec.Env) != 0 || len(spec.Mounts) != 0 {
		t.Errorf("expected zero spec, got env=%v mounts=%v", spec.Env, spec.Mounts)
	}
}

func TestSpecBuilder_withProfiles_returnsEnvAndMounts(t *testing.T) {
	awsDir := t.TempDir()
	scriptPath := filepath.Join(awsDir, "aws-creds.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	b := NewSpecBuilder("127.0.0.1:9100", "mytoken", awsDir)
	sb := config.SandboxConfig{
		Proxy: config.ProxyConfig{AWSProfiles: []string{"default", "prod"}},
	}

	spec, err := b.ContainerSpec(context.Background(), "/myproject", sb)
	if err != nil {
		t.Fatalf("ContainerSpec: %v", err)
	}

	if spec.Env["ROOST_AWS_TOKEN"] != "mytoken" {
		t.Errorf("ROOST_AWS_TOKEN = %q, want %q", spec.Env["ROOST_AWS_TOKEN"], "mytoken")
	}
	if spec.Env["ROOST_PROXY_PORT"] != "9100" {
		t.Errorf("ROOST_PROXY_PORT = %q, want %q", spec.Env["ROOST_PROXY_PORT"], "9100")
	}
	if len(spec.Mounts) != 2 {
		t.Errorf("expected 2 mounts, got %d: %v", len(spec.Mounts), spec.Mounts)
	}

	// Verify config file was written under awsDir.
	hash := projectHash("/myproject")
	configPath := filepath.Join(awsDir, "config-"+hash)
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("config file not created at %s: %v", configPath, err)
	}
}

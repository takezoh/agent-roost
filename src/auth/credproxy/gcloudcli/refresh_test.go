package gcloudcli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// stubGcloud writes a fake gcloud script to a temp dir and prepends it to PATH.
func stubGcloud(t *testing.T, token string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "gcloud")
	content := "#!/bin/sh\necho " + token + "\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write stub gcloud: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func TestRefresher_Prime_writesToken(t *testing.T) {
	stubGcloud(t, "test-access-token-abc123")
	tokenPath := filepath.Join(t.TempDir(), "access-token")

	r := NewRefresher("user@example.com", tokenPath)
	if err := r.Prime(context.Background()); err != nil {
		t.Fatalf("Prime: %v", err)
	}

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("read token: %v", err)
	}
	if string(data) != "test-access-token-abc123" {
		t.Errorf("token = %q, want %q", string(data), "test-access-token-abc123")
	}
}

func TestRefresher_Prime_atomicWrite(t *testing.T) {
	stubGcloud(t, "fresh-token")
	tokenPath := filepath.Join(t.TempDir(), "access-token")

	// Pre-seed the file with stale content.
	if err := os.WriteFile(tokenPath, []byte("stale-token"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewRefresher("user@example.com", tokenPath)
	if err := r.Prime(context.Background()); err != nil {
		t.Fatalf("Prime: %v", err)
	}

	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("read token: %v", err)
	}
	if string(data) != "fresh-token" {
		t.Errorf("token = %q, want %q", string(data), "fresh-token")
	}
}

func TestRefresher_Prime_failsWhenGcloudMissing(t *testing.T) {
	// Override PATH to an empty dir so gcloud is not found.
	t.Setenv("PATH", t.TempDir())
	tokenPath := filepath.Join(t.TempDir(), "access-token")

	r := NewRefresher("user@example.com", tokenPath)
	if err := r.Prime(context.Background()); err == nil {
		t.Fatal("expected error when gcloud is missing")
	}
}

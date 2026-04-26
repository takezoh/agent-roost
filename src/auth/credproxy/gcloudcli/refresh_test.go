package gcloudcli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestRefresher_Prime_preservesInode(t *testing.T) {
	stubGcloud(t, "fresh-token")
	tokenPath := filepath.Join(t.TempDir(), "access-token")

	// Pre-seed the file with stale content.
	if err := os.WriteFile(tokenPath, []byte("stale-token"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(tokenPath)
	if err != nil {
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

	// Inode must be preserved so Docker bind-mount consumers see the update.
	after, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Error("Prime replaced the file (new inode); Docker bind-mount will not see the update")
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

func TestRefresher_Run_fsnotify_triggersRefresh(t *testing.T) {
	stubGcloud(t, "notified-token")
	credDir := t.TempDir()
	tokenPath := filepath.Join(t.TempDir(), "access-token")

	r := NewRefresher("user@example.com", tokenPath)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		r.runWithWatcher(ctx, credDir) //nolint:errcheck
		close(done)
	}()

	// Write to the watched dir to simulate gcloud refreshing credentials.
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(credDir, "access_tokens.db"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Token file should be updated within debounce + margin.
	deadline := time.After(debounce + 500*time.Millisecond)
	for {
		select {
		case <-deadline:
			t.Fatal("token file not updated after fsnotify event")
		default:
			data, _ := os.ReadFile(tokenPath)
			if string(data) == "notified-token" {
				cancel()
				<-done
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func TestRefresher_Run_fallbackTicker(t *testing.T) {
	stubGcloud(t, "polled-token")
	tokenPath := filepath.Join(t.TempDir(), "access-token")

	r := NewRefresher("user@example.com", tokenPath)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Temporarily shorten fallback period for test speed.
	orig := fallbackPeriod
	// fallbackPeriod is a const so we test runWithTicker directly with a short ticker.
	_ = orig

	done := make(chan struct{})
	go func() {
		// Call runWithTicker directly; the real fallbackPeriod is 5m so we
		// just verify the ticker path compiles and the goroutine exits on cancel.
		cancel()
		r.runWithTicker(ctx)
		close(done)
	}()
	<-done
}

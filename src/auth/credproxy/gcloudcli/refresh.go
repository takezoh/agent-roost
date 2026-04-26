package gcloudcli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	// fallbackPeriod is a safety-net ticker used when fsnotify is unavailable
	// or when the credential directory cannot be watched.
	fallbackPeriod = 5 * time.Minute
	// debounce collapses rapid fsnotify events (e.g. SQLite WAL writes) into one call.
	debounce = 2 * time.Second
)

// Refresher keeps a token file populated with a fresh GCP access token.
// It watches the host gcloud credential store with fsnotify and calls
// gcloud auth print-access-token immediately after each internal refresh,
// falling back to a 5-minute polling ticker when the watch is unavailable.
type Refresher struct {
	account   string
	tokenPath string
}

// NewRefresher creates a Refresher that keeps tokenPath populated for account.
func NewRefresher(account, tokenPath string) *Refresher {
	return &Refresher{account: account, tokenPath: tokenPath}
}

// Prime fetches the token once synchronously. Returns an error only if the host
// gcloud invocation fails. The caller should not treat this as fatal — the token
// file will be populated on the next tick or when the user re-authenticates.
func (r *Refresher) Prime(ctx context.Context) error {
	return r.refresh(ctx)
}

// Run starts the refresh loop. It prefers fsnotify over polling:
// when the gcloud credential store changes (meaning gcloud refreshed its internal
// token), the token file is updated within debounce duration. Falls back to a
// 5-minute ticker when the watch cannot be established.
func (r *Refresher) Run(ctx context.Context) {
	credDir := gcloudCredentialDir()
	if credDir != "" {
		if err := r.runWithWatcher(ctx, credDir); err == nil {
			return
		}
	}
	r.runWithTicker(ctx)
}

func (r *Refresher) runWithWatcher(ctx context.Context, credDir string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	if err := watcher.Add(credDir); err != nil {
		return fmt.Errorf("watch %s: %w", credDir, err)
	}
	slog.Debug("gcloudcli: watching credential dir", "dir", credDir)

	var debounceTimer *time.Timer
	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return fmt.Errorf("watcher closed")
			}
			if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) {
				continue
			}
			// Debounce: SQLite WAL writes produce bursts of events.
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(debounce, func() {
				if err := r.refresh(ctx); err != nil {
					slog.Warn("gcloudcli: token refresh failed", "account", r.account, "err", err)
				}
			})
		case err, ok := <-watcher.Errors:
			if !ok {
				return fmt.Errorf("watcher error channel closed")
			}
			slog.Warn("gcloudcli: fsnotify error", "err", err)
		}
	}
}

func (r *Refresher) runWithTicker(ctx context.Context) {
	slog.Debug("gcloudcli: falling back to polling", "account", r.account, "period", fallbackPeriod)
	ticker := time.NewTicker(fallbackPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.refresh(ctx); err != nil {
				slog.Warn("gcloudcli: token refresh failed", "account", r.account, "err", err)
			}
		}
	}
}

func (r *Refresher) refresh(ctx context.Context) error {
	token, err := printAccessToken(ctx, r.account)
	if err != nil {
		return fmt.Errorf("gcloud auth print-access-token --account=%s: %w", r.account, err)
	}
	if err := writeToken(r.tokenPath, []byte(token)); err != nil {
		return fmt.Errorf("write token: %w", err)
	}
	slog.Debug("gcloudcli: token refreshed", "account", r.account)
	return nil
}

// printAccessToken shells out to gcloud to obtain a fresh access token.
func printAccessToken(ctx context.Context, account string) (string, error) {
	args := []string{"auth", "print-access-token"}
	if account != "" {
		args = append(args, "--account="+account)
	}
	out, err := exec.CommandContext(ctx, "gcloud", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// writeToken writes data to path in-place, preserving the inode so that Docker
// bind-mount consumers (containers) see the updated content. Atomic rename would
// create a new inode, leaving the bind-mounted file pointing at the old one.
func writeToken(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}

// gcloudCredentialDir returns the directory that gcloud writes credential/token
// cache files to. Returns empty string when it cannot be determined.
func gcloudCredentialDir() string {
	// Respect CLOUDSDK_CONFIG for the host process if set; otherwise use the
	// default XDG location that gcloud uses on Linux/macOS.
	if dir := os.Getenv("CLOUDSDK_CONFIG"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".config", "gcloud")
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	return dir
}

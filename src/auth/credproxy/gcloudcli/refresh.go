package gcloudcli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	tokenTTL = 60 * time.Minute
	// refreshPeriod is short so that when gcloud's internal cache expires (~1h TTL),
	// the container token file is updated within 5 minutes rather than up to 40 minutes.
	// gcloud returns the cached token until it has <60s left, then refreshes internally.
	refreshPeriod = 5 * time.Minute
)

// Refresher periodically obtains a short-lived access token from the host gcloud
// and writes it atomically to tokenPath. The token is valid for ~1h; refresh runs
// every 50 minutes so containers always have a valid token.
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

// Run starts the periodic refresh loop. Blocks until ctx is cancelled.
func (r *Refresher) Run(ctx context.Context) {
	ticker := time.NewTicker(refreshPeriod)
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

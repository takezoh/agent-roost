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
)

const (
	tokenTTL      = 60 * time.Minute
	refreshPeriod = 50 * time.Minute // refresh before TTL expires
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
	if err := atomicWrite(r.tokenPath, []byte(token)); err != nil {
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

// atomicWrite writes data to path via a temp-file rename to avoid partial reads.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".gcp-token-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

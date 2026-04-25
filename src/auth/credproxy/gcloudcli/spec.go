package gcloudcli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"sync"

	credproxy "github.com/takezoh/agent-roost/auth/credproxy"
	"github.com/takezoh/agent-roost/config"
)

// SpecBuilder implements credproxy.Provider for the gcloud CLI.
// It manages per-account token refresh goroutines and writes synthetic
// CLOUDSDK_CONFIG directories so containers receive only short-lived access tokens.
type SpecBuilder struct {
	rootCtx context.Context
	gcpDir  string // base directory; sub-dirs keyed by account hash and project hash

	mu         sync.Mutex
	refreshers map[string]*Refresher // keyed by account email
}

// NewSpecBuilder creates a SpecBuilder. gcpDir is the directory for all GCP
// materialized files (token files and synthetic CLOUDSDK_CONFIG dirs).
// rootCtx controls the lifetime of token refresh goroutines.
func NewSpecBuilder(rootCtx context.Context, gcpDir string) *SpecBuilder {
	return &SpecBuilder{
		rootCtx:    rootCtx,
		gcpDir:     gcpDir,
		refreshers: make(map[string]*Refresher),
	}
}

// ContainerSpec implements credproxy.Provider.
// Returns zero Spec when sandbox.proxy.gcp.account or .projects is empty.
func (b *SpecBuilder) ContainerSpec(ctx context.Context, projectPath string, sb config.SandboxConfig) (credproxy.Spec, error) {
	account := sb.Proxy.GCP.Account
	projects := sb.Proxy.GCP.Projects
	if account == "" || len(projects) == 0 {
		return credproxy.Spec{}, nil
	}

	tokenPath, err := b.ensureRefresher(ctx, account)
	if err != nil {
		return credproxy.Spec{}, err
	}

	configDir, err := b.writeConfigDir(projectPath, account, projects)
	if err != nil {
		return credproxy.Spec{}, err
	}

	return credproxy.Spec{
		Env:    ContainerEnv(),
		Mounts: ContainerMounts(tokenPath, configDir),
	}, nil
}

// ensureRefresher starts a token refresh goroutine for account if not already running.
// Returns the host path of the token file.
func (b *SpecBuilder) ensureRefresher(ctx context.Context, account string) (string, error) {
	accountDir := fmt.Sprintf("%s/%s", b.gcpDir, accountHash(account))
	if err := os.MkdirAll(accountDir, 0o755); err != nil {
		return "", fmt.Errorf("gcloudcli: mkdir account dir: %w", err)
	}
	tokenPath := accountDir + "/access-token"

	b.mu.Lock()
	defer b.mu.Unlock()

	if _, running := b.refreshers[account]; !running {
		ref := NewRefresher(account, tokenPath)
		if err := ref.Prime(ctx); err != nil {
			slog.Warn("gcloudcli: initial token fetch failed", "account", account, "err", err)
		}
		b.refreshers[account] = ref
		go ref.Run(b.rootCtx)
	}

	return tokenPath, nil
}

func (b *SpecBuilder) writeConfigDir(projectPath, account string, projects []string) (string, error) {
	hash := projectHash(projectPath)
	configDir := b.gcpDir + "/cloudsdk-config-" + hash
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return "", fmt.Errorf("gcloudcli: mkdir config dir: %w", err)
	}
	if err := WriteConfigDir(configDir, account, projects); err != nil {
		return "", fmt.Errorf("gcloudcli: write config dir: %w", err)
	}
	return configDir, nil
}

func accountHash(account string) string {
	h := sha256.Sum256([]byte(account))
	return hex.EncodeToString(h[:4])
}

func projectHash(projectPath string) string {
	h := sha256.Sum256([]byte(projectPath))
	return hex.EncodeToString(h[:4])
}

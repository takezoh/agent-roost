package github

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	credproxy "github.com/takezoh/agent-roost/auth/credproxy"
	"github.com/takezoh/agent-roost/config"
	credproxylib "github.com/takezoh/credproxy/pkg/credproxy"
)

// SpecBuilder implements credproxy.Provider for GitHub HTTPS authentication.
// It registers an HTTP route that shells out to "gh auth token" on the host,
// and mounts a git credential helper script and gitconfig snippet into containers.
type SpecBuilder struct {
	proxyAddr string
	token     string
	gitDir    string
}

// NewSpecBuilder creates a SpecBuilder. proxyAddr is "127.0.0.1:<port>"; gitDir
// is the directory where the helper script and gitconfig are written.
func NewSpecBuilder(proxyAddr, token, gitDir string) *SpecBuilder {
	return &SpecBuilder{proxyAddr: proxyAddr, token: token, gitDir: gitDir}
}

func (b *SpecBuilder) Name() string { return "github" }

// Init creates gitDir and writes the credential helper script and gitconfig snippet.
func (b *SpecBuilder) Init() error {
	if err := os.MkdirAll(b.gitDir, 0o755); err != nil {
		return fmt.Errorf("github: mkdir: %w", err)
	}
	if err := writeHelperScript(filepath.Join(b.gitDir, "git-credential-roost")); err != nil {
		return fmt.Errorf("github: write helper script: %w", err)
	}
	if err := writeGitconfig(filepath.Join(b.gitDir, "gitconfig")); err != nil {
		return fmt.Errorf("github: write gitconfig: %w", err)
	}
	return nil
}

// Routes returns the HTTP route that serves GitHub credentials to containers.
func (b *SpecBuilder) Routes() []credproxylib.Route {
	return []credproxylib.Route{
		{Path: RoutePath, Provider: newHTTPProvider()},
	}
}

// ContainerSpec implements credproxy.Provider.
// Returns zero Spec when proxy.github.enabled is false or gh is not installed on the host.
func (b *SpecBuilder) ContainerSpec(_ context.Context, _ string, sb config.SandboxConfig) (credproxy.Spec, error) {
	if !sb.Proxy.GitHub.Enabled {
		return credproxy.Spec{}, nil
	}

	if _, err := exec.LookPath("gh"); err != nil {
		slog.Warn("github: proxy.github.enabled=true but gh not found on host PATH")
		return credproxy.Spec{}, nil
	}

	helperPath := filepath.Join(b.gitDir, "git-credential-roost")
	gitconfigPath := filepath.Join(b.gitDir, "gitconfig")

	return credproxy.Spec{
		Env: containerEnv(b.proxyAddr, b.token),
		Mounts: []string{
			helperPath + ":" + containerHelperPath + ":ro",
			gitconfigPath + ":" + containerGitconfigPath + ":ro",
		},
	}, nil
}

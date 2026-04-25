package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"

	credproxy "github.com/takezoh/agent-roost/auth/credproxy"
	"github.com/takezoh/agent-roost/auth/credproxy/awssso"
	"github.com/takezoh/agent-roost/auth/credproxy/gcloudcli"
	"github.com/takezoh/agent-roost/config"
	credproxylib "github.com/takezoh/credproxy/pkg/credproxy"
)

// CredProxyRunner holds an in-process credential proxy server and a set of
// provider-specific SpecBuilders. Each provider encapsulates all knowledge of
// its credential system; this runner fans out ContainerSpec calls and merges results.
type CredProxyRunner struct {
	srv       *credproxylib.Server
	providers []credproxy.Provider
}

// StartCredProxy starts an in-process credential proxy and registers all built-in
// providers. The returned runner's ContainerSpec method is the only entry point
// for docker_launcher — it contains no provider-specific logic.
func StartCredProxy(ctx context.Context, dataDir string) (*CredProxyRunner, error) {
	token, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("credproxy: generate token: %w", err)
	}

	routes := []credproxylib.Route{
		{
			Path:     awssso.RoutePath,
			Provider: awssso.New(),
		},
	}

	srv, err := credproxylib.New(credproxylib.ServerConfig{
		ListenTCP:  "127.0.0.1:0",
		AuthTokens: []string{token},
		Routes:     routes,
	})
	if err != nil {
		return nil, fmt.Errorf("credproxy: create server: %w", err)
	}

	awsDir := filepath.Join(dataDir, "aws")
	if err := os.MkdirAll(awsDir, 0o755); err != nil {
		return nil, fmt.Errorf("credproxy: make aws dir: %w", err)
	}
	if err := awssso.WriteHelperScript(filepath.Join(awsDir, "aws-creds.sh")); err != nil {
		return nil, fmt.Errorf("credproxy: write helper script: %w", err)
	}

	gcpDir := filepath.Join(dataDir, "gcp")
	if err := os.MkdirAll(gcpDir, 0o755); err != nil {
		return nil, fmt.Errorf("credproxy: make gcp dir: %w", err)
	}

	_, port, _ := net.SplitHostPort(srv.Addr())
	proxyAddr := "127.0.0.1:" + port

	providers := []credproxy.Provider{
		awssso.NewSpecBuilder(proxyAddr, token, awsDir),
		gcloudcli.NewSpecBuilder(ctx, gcpDir),
	}

	go func() { _ = srv.Run(ctx) }()

	return &CredProxyRunner{srv: srv, providers: providers}, nil
}

// ContainerSpec fans out to all providers and merges their Env and Mounts.
// Provider errors are logged as warnings and do not abort the other providers.
func (r *CredProxyRunner) ContainerSpec(ctx context.Context, projectPath string, sb config.SandboxConfig) (credproxy.Spec, error) {
	out := credproxy.Spec{Env: map[string]string{}}
	for _, p := range r.providers {
		s, err := p.ContainerSpec(ctx, projectPath, sb)
		if err != nil {
			slog.Warn("credproxy: provider failed", "project", projectPath, "err", err)
			continue
		}
		for k, v := range s.Env {
			out.Env[k] = v
		}
		out.Mounts = append(out.Mounts, s.Mounts...)
	}
	return out, nil
}

func generateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

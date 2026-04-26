package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"

	credproxy "github.com/takezoh/agent-roost/auth/credproxy"
	"github.com/takezoh/agent-roost/auth/credproxy/awssso"
	"github.com/takezoh/agent-roost/auth/credproxy/gcloudcli"
	"github.com/takezoh/agent-roost/auth/credproxy/github"
	"github.com/takezoh/agent-roost/auth/credproxy/sshagent"
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

	// Build providers with proxyAddr="" for the first pass: Routes() does not
	// depend on proxyAddr (HTTP handlers run on the server side), so we can
	// collect routes before the server is started and the port is known.
	awsSpec := awssso.NewSpecBuilder("", token, dataDir+"/aws")
	ghSpec := github.NewSpecBuilder("", token, dataDir+"/git")
	gcpSpec := gcloudcli.NewSpecBuilder(ctx, dataDir+"/gcp")
	sshSpec := sshagent.NewSpecBuilder()

	earlyProviders := []credproxy.Provider{awsSpec, gcpSpec, sshSpec, ghSpec}
	var routes []credproxylib.Route
	for _, p := range earlyProviders {
		routes = append(routes, p.Routes()...)
	}

	srv, err := credproxylib.New(credproxylib.ServerConfig{
		ListenTCP:  "127.0.0.1:0",
		AuthTokens: []string{token},
		Routes:     routes,
	})
	if err != nil {
		return nil, fmt.Errorf("credproxy: create server: %w", err)
	}

	_, port, _ := net.SplitHostPort(srv.Addr())
	proxyAddr := "127.0.0.1:" + port

	// Rebuild providers that need the resolved proxy address for ContainerSpec env.
	providers := []credproxy.Provider{
		awssso.NewSpecBuilder(proxyAddr, token, dataDir+"/aws"),
		gcloudcli.NewSpecBuilder(ctx, dataDir+"/gcp"),
		sshagent.NewSpecBuilder(),
		github.NewSpecBuilder(proxyAddr, token, dataDir+"/git"),
	}

	for _, p := range providers {
		if err := p.Init(); err != nil {
			return nil, fmt.Errorf("credproxy: provider %s init: %w", p.Name(), err)
		}
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
			slog.Warn("credproxy: provider failed", "provider", p.Name(), "project", projectPath, "err", err)
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

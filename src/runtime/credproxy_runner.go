package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"

	"github.com/takezoh/agent-roost/auth/credproxy/awssso"
	credproxylib "github.com/takezoh/credproxy/pkg/credproxy"
)

// CredProxyRunner holds an in-process AWS SSO credential proxy server instance.
// The server listens on an ephemeral TCP port; containers reach it via host.docker.internal.
// Containers use AWS_CONTAINER_CREDENTIALS_FULL_URI to obtain short-lived credentials
// without exposing ~/.aws/sso/cache to the container.
type CredProxyRunner struct {
	srv   *credproxylib.Server
	addr  string // resolved "host:port" after listen
	token string // ephemeral bearer token, valid for the process lifetime
}

// StartCredProxy starts an in-process AWS SSO credential proxy.
// The caller is responsible for keeping ctx alive for the duration of the proxy.
func StartCredProxy(ctx context.Context) (*CredProxyRunner, error) {
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

	go func() { _ = srv.Run(ctx) }()

	return &CredProxyRunner{srv: srv, addr: srv.Addr(), token: token}, nil
}

// ContainerEnv returns the env vars a container must set to reach this proxy.
// Callers merge the returned map into StartOptions.Env without inspecting the keys;
// provider-specific env var names (AWS_*, etc.) are resolved inside auth/credproxy/<provider>/.
func (r *CredProxyRunner) ContainerEnv() map[string]string {
	_, port, _ := net.SplitHostPort(r.addr)
	base := "http://host.docker.internal:" + port
	return awssso.ContainerEnv(base, r.token)
}

func generateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

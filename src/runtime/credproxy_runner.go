package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"

	"github.com/takezoh/agent-roost/auth/credproxy/anthropicoauth"
	"github.com/takezoh/agent-roost/auth/credproxy/awssso"
	credproxylib "github.com/takezoh/credproxy/pkg/credproxy"
)

// CredProxyRunner holds an in-process credential proxy server instance.
// The server listens on an ephemeral TCP port; containers reach it via host.docker.internal.
type CredProxyRunner struct {
	srv   *credproxylib.Server
	addr  string // resolved "host:port" after listen
	token string // ephemeral bearer token, valid for the process lifetime
}

// StartCredProxy starts an in-process credential proxy.
// The Anthropic provider reads ~/.claude/.credentials.json on the host;
// no per-tool credential store is needed.
// The caller is responsible for keeping ctx alive for the duration of the proxy.
func StartCredProxy(ctx context.Context) (*CredProxyRunner, error) {
	token, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("credproxy: generate token: %w", err)
	}

	routes := []credproxylib.Route{
		{
			Path:             "/anthropic",
			Upstream:         "https://api.anthropic.com",
			Provider:         anthropicoauth.New(),
			RefreshOnStatus:  []int{401},
			StripInboundAuth: true,
		},
		{
			Path:     "/aws-credentials",
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

// portOf extracts the port from a "host:port" address string.
func portOf(addr string) string {
	_, port, _ := net.SplitHostPort(addr)
	return port
}

func generateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

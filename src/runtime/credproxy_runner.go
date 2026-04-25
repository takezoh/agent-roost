package runtime

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/takezoh/agent-roost/auth/credproxy/awssso"
	credproxylib "github.com/takezoh/credproxy/pkg/credproxy"
)

// CredProxyRunner holds an in-process AWS SSO credential proxy server instance.
// The server listens on an ephemeral TCP port; containers reach it via host.docker.internal.
// Containers receive a synthetic ~/.aws/config that uses credential_process to fetch
// per-profile short-lived credentials without exposing ~/.aws/sso/cache to the container.
type CredProxyRunner struct {
	srv        *credproxylib.Server
	addr       string // resolved "host:port" after listen
	token      string // ephemeral bearer token, valid for the process lifetime
	dataDir    string // root directory for materialized files
	scriptPath string // host path of the materialized helper script
}

// StartCredProxy starts an in-process AWS SSO credential proxy and materializes
// the container-side helper script into dataDir/aws/.
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
	scriptPath := filepath.Join(awsDir, "aws-creds.sh")
	if err := awssso.WriteHelperScript(scriptPath); err != nil {
		return nil, fmt.Errorf("credproxy: write helper script: %w", err)
	}

	go func() { _ = srv.Run(ctx) }()

	return &CredProxyRunner{
		srv:        srv,
		addr:       srv.Addr(),
		token:      token,
		dataDir:    dataDir,
		scriptPath: scriptPath,
	}, nil
}

// ContainerEnv returns the env vars a container must set to reach this proxy.
// Callers merge the returned map into StartOptions.Env without inspecting the keys;
// provider-specific env var names are resolved inside auth/credproxy/<provider>/.
func (r *CredProxyRunner) ContainerEnv() map[string]string {
	_, port, _ := net.SplitHostPort(r.addr)
	base := "http://host.docker.internal:" + port
	return awssso.ContainerEnv(base, r.token)
}

// ContainerMounts returns bind-mount specs for the synthetic ~/.aws/config and helper script.
// profiles is the per-project list from sandbox.proxy.aws_profiles in the project settings.
// homeDir is the in-container home directory (same as host home: the container runs as
// the host user with -e HOME=...).
// Returns nil when profiles is empty.
func (r *CredProxyRunner) ContainerMounts(projectPath, homeDir string, profiles []string) ([]string, error) {
	if len(profiles) == 0 {
		return nil, nil
	}

	configPath, err := r.renderProjectConfig(projectPath, profiles)
	if err != nil {
		return nil, err
	}

	return []string{
		configPath + ":" + filepath.Join(homeDir, ".aws", "config") + ":ro",
		r.scriptPath + ":/opt/roost/aws-creds:ro",
	}, nil
}

// renderProjectConfig writes (or overwrites) the synthetic ~/.aws/config for projectPath
// and returns its host-side path.
func (r *CredProxyRunner) renderProjectConfig(projectPath string, profiles []string) (string, error) {
	hash := projectHash(projectPath)
	configPath := filepath.Join(r.dataDir, "aws", "config-"+hash)

	var buf bytes.Buffer
	if err := awssso.RenderConfig(&buf, profiles, "/opt/roost/aws-creds"); err != nil {
		return "", fmt.Errorf("credproxy: render config for %s: %w", projectPath, err)
	}
	if err := os.WriteFile(configPath, buf.Bytes(), 0o644); err != nil {
		return "", fmt.Errorf("credproxy: write config for %s: %w", projectPath, err)
	}
	return configPath, nil
}

// projectHash returns an 8-char hex prefix of SHA-256(projectPath) for use in filenames.
func projectHash(projectPath string) string {
	h := sha256.Sum256([]byte(projectPath))
	return hex.EncodeToString(h[:4])
}

func generateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

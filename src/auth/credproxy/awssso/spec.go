package awssso

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"

	credproxy "github.com/takezoh/agent-roost/auth/credproxy"
	"github.com/takezoh/agent-roost/config"
	credproxylib "github.com/takezoh/credproxy/pkg/credproxy"
)

// SpecBuilder implements credproxy.Provider for AWS SSO.
// It generates a synthetic ~/.aws/config per project when aws_profiles are configured.
type SpecBuilder struct {
	proxyAddr string // "host:port" from credproxylib.Server.Addr()
	token     string
	awsDir    string // directory where config-<hash> files and aws-creds.sh live
}

// NewSpecBuilder creates a SpecBuilder. awsDir is the directory where the
// aws-creds.sh helper and per-project config files are stored.
func NewSpecBuilder(proxyAddr, token, awsDir string) *SpecBuilder {
	return &SpecBuilder{proxyAddr: proxyAddr, token: token, awsDir: awsDir}
}

func (b *SpecBuilder) Name() string { return "awssso" }

// Init creates awsDir and materializes the credential helper script.
func (b *SpecBuilder) Init() error {
	if err := os.MkdirAll(b.awsDir, 0o755); err != nil {
		return fmt.Errorf("awssso: mkdir: %w", err)
	}
	return WriteHelperScript(filepath.Join(b.awsDir, "aws-creds.sh"))
}

// Routes returns the HTTP route that serves AWS credentials to containers.
func (b *SpecBuilder) Routes() []credproxylib.Route {
	return []credproxylib.Route{
		{Path: RoutePath, Provider: New()},
	}
}

// ContainerSpec implements credproxy.Provider.
// Returns zero Spec when sandbox.proxy.aws_profiles is empty.
func (b *SpecBuilder) ContainerSpec(_ context.Context, projectPath string, sb config.SandboxConfig) (credproxy.Spec, error) {
	profiles := sb.Proxy.AWSProfiles
	if len(profiles) == 0 {
		return credproxy.Spec{}, nil
	}

	configPath, err := b.renderProjectConfig(projectPath, profiles)
	if err != nil {
		return credproxy.Spec{}, err
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return credproxy.Spec{}, fmt.Errorf("awssso: get home dir: %w", err)
	}

	_, port, _ := net.SplitHostPort(b.proxyAddr)
	env := ContainerEnv("http://host.docker.internal:"+port, b.token)

	scriptPath := filepath.Join(b.awsDir, "aws-creds.sh")
	mounts := []string{
		configPath + ":" + filepath.Join(homeDir, ".aws", "config") + ":ro",
		scriptPath + ":/opt/roost/aws-creds:ro",
	}

	return credproxy.Spec{Env: env, Mounts: mounts}, nil
}

func (b *SpecBuilder) renderProjectConfig(projectPath string, profiles []string) (string, error) {
	hash := projectHash(projectPath)
	configPath := filepath.Join(b.awsDir, "config-"+hash)

	var buf bytes.Buffer
	if err := RenderConfig(&buf, profiles, "/opt/roost/aws-creds"); err != nil {
		return "", fmt.Errorf("awssso: render config for %s: %w", projectPath, err)
	}
	if err := os.WriteFile(configPath, buf.Bytes(), 0o644); err != nil {
		return "", fmt.Errorf("awssso: write config for %s: %w", projectPath, err)
	}
	return configPath, nil
}

func projectHash(projectPath string) string {
	h := sha256.Sum256([]byte(projectPath))
	return hex.EncodeToString(h[:4])
}

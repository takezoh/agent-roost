// Package anthropicoauth provides a credproxy.Provider that injects the
// Anthropic OAuth access token from Claude Code's host credentials file
// (~/.claude/.credentials.json).
//
// Refresh is delegated to Claude Code on the host: when the token expires
// the user re-authenticates with the `claude` CLI, which rewrites the file;
// the next request through this provider picks up the new token.
package anthropicoauth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	credproxylib "github.com/takezoh/credproxy/pkg/credproxy"
)

// hostCredentialsFile is the JSON file Claude Code writes on the host.
type hostCredentialsFile struct {
	ClaudeAiOauth struct {
		AccessToken string `json:"accessToken"`
		ExpiresAt   int64  `json:"expiresAt"` // Unix milliseconds
	} `json:"claudeAiOauth"`
}

// Provider implements credproxy.Provider by reading the host credentials
// file on every Get/Refresh call.
type Provider struct {
	path string
}

// New returns a Provider that reads ~/.claude/.credentials.json.
func New() *Provider {
	home, _ := os.UserHomeDir()
	return &Provider{path: filepath.Join(home, ".claude", ".credentials.json")}
}

// NewWithPath returns a Provider reading from a specific path. Used in tests.
func NewWithPath(path string) *Provider {
	return &Provider{path: path}
}

func (p *Provider) Get(_ context.Context, _ credproxylib.Request) (*credproxylib.Injection, error) {
	return p.read()
}

// Refresh re-reads the file. Claude Code may have updated the token on the
// host since our last read; the freshest content wins.
func (p *Provider) Refresh(_ context.Context, _ credproxylib.Request) (*credproxylib.Injection, error) {
	return p.read()
}

func (p *Provider) read() (*credproxylib.Injection, error) {
	data, err := os.ReadFile(p.path)
	if err != nil {
		return nil, fmt.Errorf("anthropicoauth: read %s: %w", p.path, err)
	}
	var creds hostCredentialsFile
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("anthropicoauth: parse %s: %w", p.path, err)
	}
	if creds.ClaudeAiOauth.AccessToken == "" {
		return nil, fmt.Errorf("anthropicoauth: no claudeAiOauth.accessToken in %s; run `claude` on host to authenticate", p.path)
	}
	inj := &credproxylib.Injection{
		Headers: map[string]string{"Authorization": "Bearer " + creds.ClaudeAiOauth.AccessToken},
	}
	if creds.ClaudeAiOauth.ExpiresAt > 0 {
		inj.ExpiresAt = time.UnixMilli(creds.ClaudeAiOauth.ExpiresAt)
	}
	return inj, nil
}

package awssso

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	credproxylib "github.com/takezoh/credproxy/pkg/credproxy"
)

// RoutePath is the proxy path prefix served by this provider.
// Requests arrive as /aws-credentials/<profile>; the library strips the prefix
// so Provider receives Request.Path = "/<profile>".
// Generic layers use this constant to avoid hard-coding the string.
const RoutePath = "/aws-credentials"

// ContainerEnv returns the env vars a container must set to reach this proxy.
// base is "http://host.docker.internal:<port>"; token is the ephemeral bearer token.
// Keys are roost-internal names (ROOST_*) so tool-specific AWS literals never
// appear in runtime/ or sandbox/.
func ContainerEnv(base, token string) map[string]string {
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(base, "http://"))
	return map[string]string{
		"ROOST_AWS_TOKEN":  token,
		"ROOST_PROXY_PORT": port,
	}
}

// processCredentials is the JSON format required by credential_process.
// AWS SDKs validate Version == 1.
type processCredentials struct {
	Version         int    `json:"Version"`
	AccessKeyId     string `json:"AccessKeyId"`
	SecretAccessKey string `json:"SecretAccessKey"`
	SessionToken    string `json:"SessionToken,omitempty"`
	Expiration      string `json:"Expiration,omitempty"`
}

type cachedCreds struct {
	body    []byte
	expires time.Time
}

// Provider implements credproxy.Provider for AWS SSO.
// It shells out to the aws CLI to obtain temporary credentials per profile and
// serves them as a credential_process-compatible JSON body (BodyReplace).
// Credentials are cached per profile with a 60-second early-refresh margin.
type Provider struct {
	mu    sync.Mutex
	cache map[string]*cachedCreds // keyed by profile name ("" = default)
}

const refreshMargin = 60 * time.Second

// New creates an AWSSSOProvider.
func New() *Provider { return &Provider{cache: make(map[string]*cachedCreds)} }

func (p *Provider) Get(ctx context.Context, req credproxylib.Request) (*credproxylib.Injection, error) {
	profile := profileFromPath(req.Path)
	p.mu.Lock()
	if c := p.cache[profile]; c != nil && time.Now().Add(refreshMargin).Before(c.expires) {
		body := c.body
		p.mu.Unlock()
		return &credproxylib.Injection{BodyReplace: body}, nil
	}
	p.mu.Unlock()
	return p.fetch(ctx, profile)
}

func (p *Provider) Refresh(ctx context.Context, req credproxylib.Request) (*credproxylib.Injection, error) {
	profile := profileFromPath(req.Path)
	p.mu.Lock()
	delete(p.cache, profile)
	p.mu.Unlock()
	return p.fetch(ctx, profile)
}

func (p *Provider) fetch(ctx context.Context, profile string) (*credproxylib.Injection, error) {
	creds, expires, err := obtainCredentials(ctx, profile)
	if err != nil {
		return nil, fmt.Errorf("awssso: %w", err)
	}

	body, err := json.Marshal(creds)
	if err != nil {
		return nil, fmt.Errorf("awssso: marshal: %w", err)
	}

	p.mu.Lock()
	p.cache[profile] = &cachedCreds{body: body, expires: expires}
	p.mu.Unlock()

	return &credproxylib.Injection{BodyReplace: body, ExpiresAt: expires}, nil
}

// profileFromPath extracts the profile name from the stripped request path.
// The library strips RoutePath, so req.Path is "/<profile>" or "/".
// Empty or "/" maps to "" (= default profile, no --profile flag).
func profileFromPath(path string) string {
	p := strings.TrimPrefix(path, "/")
	if p == "default" {
		return ""
	}
	return p
}

// obtainCredentials tries, in order:
//  1. aws configure export-credentials (works with any credential source)
//  2. aws sso get-role-credentials via the SSO cache
func obtainCredentials(ctx context.Context, profile string) (processCredentials, time.Time, error) {
	if creds, exp, err := exportCredentials(ctx, profile); err == nil {
		return creds, exp, nil
	}
	return ssoCredentials(ctx)
}

// exportCredentials runs "aws configure export-credentials --format process".
// profile is passed as --profile when non-empty; "" uses the default credential chain.
func exportCredentials(ctx context.Context, profile string) (processCredentials, time.Time, error) {
	args := []string{"configure", "export-credentials", "--format", "process"}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	var stdout bytes.Buffer
	c := exec.CommandContext(ctx, "aws", args...)
	c.Stdout = &stdout
	if err := c.Run(); err != nil {
		return processCredentials{}, time.Time{}, err
	}

	// process format: {"Version":1,"AccessKeyId":...,"SecretAccessKey":...,"SessionToken":...,"Expiration":...}
	var raw struct {
		AccessKeyId     string `json:"AccessKeyId"`
		SecretAccessKey string `json:"SecretAccessKey"`
		SessionToken    string `json:"SessionToken"`
		Expiration      string `json:"Expiration"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		return processCredentials{}, time.Time{}, err
	}
	if raw.AccessKeyId == "" {
		return processCredentials{}, time.Time{}, fmt.Errorf("export-credentials: no AccessKeyId")
	}

	return processCredentials{
		Version:         1,
		AccessKeyId:     raw.AccessKeyId,
		SecretAccessKey: raw.SecretAccessKey,
		SessionToken:    raw.SessionToken,
		Expiration:      raw.Expiration,
	}, parseExpiration(raw.Expiration), nil
}

// ssoCredentials reads ~/.aws/sso/cache/*.json and calls aws sso get-role-credentials.
// Used as a fallback when export-credentials fails (e.g. legacy SSO config without sso-session).
func ssoCredentials(ctx context.Context) (processCredentials, time.Time, error) {
	home, _ := os.UserHomeDir()
	cacheDir := filepath.Join(home, ".aws", "sso", "cache")

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return processCredentials{}, time.Time{}, fmt.Errorf("sso cache dir: %w", err)
	}

	now := time.Now()
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cacheDir, e.Name()))
		if err != nil {
			continue
		}

		var token struct {
			AccessToken string `json:"accessToken"`
			ExpiresAt   string `json:"expiresAt"`
			AccountId   string `json:"accountId"`
			RoleName    string `json:"roleName"`
		}
		if err := json.Unmarshal(data, &token); err != nil {
			continue
		}
		if token.AccessToken == "" || token.AccountId == "" || token.RoleName == "" {
			continue
		}
		exp := parseExpiration(token.ExpiresAt)
		if !exp.IsZero() && exp.Before(now) {
			continue
		}

		creds, expires, err := getRoleCredentials(ctx, token.AccountId, token.RoleName, token.AccessToken)
		if err != nil {
			continue
		}
		return creds, expires, nil
	}

	return processCredentials{}, time.Time{}, fmt.Errorf("no valid SSO session found; run `aws sso login`")
}

func getRoleCredentials(ctx context.Context, accountID, roleName, accessToken string) (processCredentials, time.Time, error) {
	var stdout bytes.Buffer
	c := exec.CommandContext(ctx, "aws", "sso", "get-role-credentials",
		"--account-id", accountID,
		"--role-name", roleName,
		"--access-token", accessToken,
		"--output", "json",
	)
	c.Stdout = &stdout
	if err := c.Run(); err != nil {
		return processCredentials{}, time.Time{}, err
	}

	var result struct {
		RoleCredentials struct {
			AccessKeyId     string `json:"accessKeyId"`
			SecretAccessKey string `json:"secretAccessKey"`
			SessionToken    string `json:"sessionToken"`
			Expiration      int64  `json:"expiration"` // Unix milliseconds
		} `json:"roleCredentials"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return processCredentials{}, time.Time{}, err
	}
	rc := result.RoleCredentials
	if rc.AccessKeyId == "" {
		return processCredentials{}, time.Time{}, fmt.Errorf("get-role-credentials: no AccessKeyId")
	}

	var expires time.Time
	var expStr string
	if rc.Expiration > 0 {
		expires = time.UnixMilli(rc.Expiration)
		expStr = expires.UTC().Format(time.RFC3339)
	}

	return processCredentials{
		Version:         1,
		AccessKeyId:     rc.AccessKeyId,
		SecretAccessKey: rc.SecretAccessKey,
		SessionToken:    rc.SessionToken,
		Expiration:      expStr,
	}, expires, nil
}

func parseExpiration(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

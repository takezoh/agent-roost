package awssso

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	credproxylib "github.com/takezoh/credproxy/pkg/credproxy"
)

// RoutePath is the proxy path served by this provider.
// Generic layers use this constant instead of hard-coding "/aws-credentials".
const RoutePath = "/aws-credentials"

// ContainerEnv returns the env vars a container must set to reach this provider via proxy.
// baseURL is "http://host.docker.internal:<port>" and token is the ephemeral bearer token.
// Keeping these names here ensures AWS-specific env var literals never appear in runtime/ or sandbox/.
func ContainerEnv(baseURL, token string) map[string]string {
	return map[string]string{
		"AWS_CONTAINER_CREDENTIALS_FULL_URI": baseURL + RoutePath,
		"AWS_CONTAINER_AUTHORIZATION_TOKEN":  token,
	}
}

// imdsCredentials is the JSON format expected by AWS_CONTAINER_CREDENTIALS_FULL_URI.
type imdsCredentials struct {
	AccessKeyId     string `json:"AccessKeyId"`
	SecretAccessKey string `json:"SecretAccessKey"`
	Token           string `json:"Token"`
	Expiration      string `json:"Expiration"`
}

type cachedCreds struct {
	body    []byte
	expires time.Time
}

// Provider implements credproxy.Provider for AWS SSO.
// It shells out to the aws CLI to obtain temporary credentials and serves them
// as an IMDS-compatible JSON body (BodyReplace).
type Provider struct {
	mu    sync.Mutex
	cache *cachedCreds
}

const refreshMargin = 60 * time.Second

// New creates an AWSSSOProvider.
func New() *Provider { return &Provider{} }

func (p *Provider) Get(ctx context.Context, _ credproxylib.Request) (*credproxylib.Injection, error) {
	p.mu.Lock()
	if p.cache != nil && time.Now().Add(refreshMargin).Before(p.cache.expires) {
		body := p.cache.body
		p.mu.Unlock()
		return &credproxylib.Injection{BodyReplace: body}, nil
	}
	p.mu.Unlock()

	return p.fetch(ctx)
}

func (p *Provider) Refresh(ctx context.Context, _ credproxylib.Request) (*credproxylib.Injection, error) {
	p.mu.Lock()
	p.cache = nil
	p.mu.Unlock()
	return p.fetch(ctx)
}

func (p *Provider) fetch(ctx context.Context) (*credproxylib.Injection, error) {
	creds, expires, err := obtainCredentials(ctx)
	if err != nil {
		return nil, fmt.Errorf("awssso: %w", err)
	}

	body, err := json.Marshal(creds)
	if err != nil {
		return nil, fmt.Errorf("awssso: marshal: %w", err)
	}

	p.mu.Lock()
	p.cache = &cachedCreds{body: body, expires: expires}
	p.mu.Unlock()

	return &credproxylib.Injection{BodyReplace: body, ExpiresAt: expires}, nil
}

// obtainCredentials tries, in order:
//  1. aws configure export-credentials (works with any credential source)
//  2. aws sso get-role-credentials via the SSO cache
func obtainCredentials(ctx context.Context) (imdsCredentials, time.Time, error) {
	// Try the simplest path first: let the AWS CLI resolve credentials itself.
	if creds, exp, err := exportCredentials(ctx); err == nil {
		return creds, exp, nil
	}

	// Fall back to SSO cache.
	return ssoCredentials(ctx)
}

// exportCredentials runs "aws configure export-credentials --format process".
func exportCredentials(ctx context.Context) (imdsCredentials, time.Time, error) {
	var stdout bytes.Buffer
	c := exec.CommandContext(ctx, "aws", "configure", "export-credentials", "--format", "process")
	c.Stdout = &stdout
	if err := c.Run(); err != nil {
		return imdsCredentials{}, time.Time{}, err
	}

	// process format: {"Version":1,"AccessKeyId":...,"SecretAccessKey":...,"SessionToken":...,"Expiration":...}
	var raw struct {
		AccessKeyId     string `json:"AccessKeyId"`
		SecretAccessKey string `json:"SecretAccessKey"`
		SessionToken    string `json:"SessionToken"`
		Expiration      string `json:"Expiration"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		return imdsCredentials{}, time.Time{}, err
	}
	if raw.AccessKeyId == "" {
		return imdsCredentials{}, time.Time{}, fmt.Errorf("export-credentials: no AccessKeyId")
	}

	expires := parseExpiration(raw.Expiration)
	return imdsCredentials{
		AccessKeyId:     raw.AccessKeyId,
		SecretAccessKey: raw.SecretAccessKey,
		Token:           raw.SessionToken,
		Expiration:      raw.Expiration,
	}, expires, nil
}

// ssoCredentials reads ~/.aws/sso/cache/*.json and calls aws sso get-role-credentials.
func ssoCredentials(ctx context.Context) (imdsCredentials, time.Time, error) {
	home, _ := os.UserHomeDir()
	cacheDir := filepath.Join(home, ".aws", "sso", "cache")

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return imdsCredentials{}, time.Time{}, fmt.Errorf("sso cache dir: %w", err)
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

	return imdsCredentials{}, time.Time{}, fmt.Errorf("no valid SSO session found; run `aws sso login`")
}

func getRoleCredentials(ctx context.Context, accountID, roleName, accessToken string) (imdsCredentials, time.Time, error) {
	var stdout bytes.Buffer
	c := exec.CommandContext(ctx, "aws", "sso", "get-role-credentials",
		"--account-id", accountID,
		"--role-name", roleName,
		"--access-token", accessToken,
		"--output", "json",
	)
	c.Stdout = &stdout
	if err := c.Run(); err != nil {
		return imdsCredentials{}, time.Time{}, err
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
		return imdsCredentials{}, time.Time{}, err
	}
	rc := result.RoleCredentials
	if rc.AccessKeyId == "" {
		return imdsCredentials{}, time.Time{}, fmt.Errorf("get-role-credentials: no AccessKeyId")
	}

	var expires time.Time
	var expStr string
	if rc.Expiration > 0 {
		expires = time.UnixMilli(rc.Expiration)
		expStr = expires.UTC().Format(time.RFC3339)
	}

	return imdsCredentials{
		AccessKeyId:     rc.AccessKeyId,
		SecretAccessKey: rc.SecretAccessKey,
		Token:           rc.SessionToken,
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

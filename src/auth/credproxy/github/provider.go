package github

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	credproxylib "github.com/takezoh/credproxy/pkg/credproxy"
)

const tokenCacheTTL = 5 * time.Minute

// httpProvider implements credproxylib.Provider for the /git-credentials route.
// It shells out to "gh auth token" on the host and returns git credential helper
// protocol output ("username=oauth2\npassword=<token>\n").
type httpProvider struct {
	mu        sync.Mutex
	cached    string
	expiresAt time.Time
}

func newHTTPProvider() *httpProvider { return &httpProvider{} }

func (p *httpProvider) Get(ctx context.Context, _ credproxylib.Request) (*credproxylib.Injection, error) {
	p.mu.Lock()
	if p.cached != "" && time.Now().Before(p.expiresAt) {
		body := p.cached
		p.mu.Unlock()
		return &credproxylib.Injection{BodyReplace: []byte(body)}, nil
	}
	p.mu.Unlock()
	return p.fetch(ctx)
}

func (p *httpProvider) Refresh(ctx context.Context, _ credproxylib.Request) (*credproxylib.Injection, error) {
	p.mu.Lock()
	p.cached = ""
	p.mu.Unlock()
	return p.fetch(ctx)
}

func (p *httpProvider) fetch(ctx context.Context) (*credproxylib.Injection, error) {
	out, err := exec.CommandContext(ctx, "gh", "auth", "token", "--hostname", "github.com").Output()
	if err != nil {
		return nil, fmt.Errorf("github: gh auth token: %w", err)
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return nil, fmt.Errorf("github: gh auth token returned empty output")
	}

	// git credential helper protocol response
	body := "username=oauth2\npassword=" + token + "\n"

	p.mu.Lock()
	p.cached = body
	p.expiresAt = time.Now().Add(tokenCacheTTL)
	p.mu.Unlock()

	return &credproxylib.Injection{BodyReplace: []byte(body)}, nil
}

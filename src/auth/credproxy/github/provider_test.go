package github

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	credproxylib "github.com/takezoh/credproxy/pkg/credproxy"
)

func stubGhToken(t *testing.T, token string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "gh")
	content := "#!/bin/sh\necho " + token + "\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func fakeReq() credproxylib.Request {
	return credproxylib.Request{Method: "POST", Path: "/"}
}

func TestHTTPProvider_get_returns_credential_format(t *testing.T) {
	stubGhToken(t, "ghp_faketoken123")

	p := newHTTPProvider()
	inj, err := p.Get(context.Background(), fakeReq())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	want := "username=oauth2\npassword=ghp_faketoken123\n"
	if string(inj.BodyReplace) != want {
		t.Errorf("body = %q, want %q", string(inj.BodyReplace), want)
	}
}

func TestHTTPProvider_caches_token(t *testing.T) {
	stubGhToken(t, "ghp_cached")

	p := newHTTPProvider()
	if _, err := p.Get(context.Background(), fakeReq()); err != nil {
		t.Fatal(err)
	}
	inj2, err := p.Get(context.Background(), fakeReq())
	if err != nil {
		t.Fatal(err)
	}
	want := "username=oauth2\npassword=ghp_cached\n"
	if string(inj2.BodyReplace) != want {
		t.Errorf("second call body = %q, want %q", string(inj2.BodyReplace), want)
	}
}

func TestHTTPProvider_refresh_bypasses_cache(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "gh")
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))

	if err := os.WriteFile(script, []byte("#!/bin/sh\necho ghp_tokenA\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	p := newHTTPProvider()
	if _, err := p.Get(context.Background(), fakeReq()); err != nil {
		t.Fatal(err)
	}

	// Replace stub to return token B; Refresh must bypass cache.
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho ghp_tokenB\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	inj, err := p.Refresh(context.Background(), fakeReq())
	if err != nil {
		t.Fatal(err)
	}
	want := "username=oauth2\npassword=ghp_tokenB\n"
	if string(inj.BodyReplace) != want {
		t.Errorf("Refresh body = %q, want %q", string(inj.BodyReplace), want)
	}
}

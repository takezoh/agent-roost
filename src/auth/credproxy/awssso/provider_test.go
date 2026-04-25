package awssso

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	credproxylib "github.com/takezoh/credproxy/pkg/credproxy"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAWSScript responds to "configure export-credentials --format process [--profile <name>]"
// with credentials whose AccessKeyId encodes the profile for assertion.
const fakeAWSScript = `#!/bin/sh
profile=""
i=1
while [ $i -le $# ]; do
  eval "arg=\${$i}"
  if [ "$arg" = "--profile" ]; then
    i=$((i+1))
    eval "profile=\${$i}"
  fi
  i=$((i+1))
done
case "$1 $2 $3 $4" in
  "configure export-credentials --format process")
    if [ -z "$profile" ]; then profile="default"; fi
    echo "{\"Version\":1,\"AccessKeyId\":\"ID-$profile\",\"SecretAccessKey\":\"SECRET\",\"SessionToken\":\"TOKEN\",\"Expiration\":\"2099-01-01T00:00:00Z\"}"
    ;;
  *)
    echo "unknown args: $*" >&2
    exit 1
    ;;
esac
`

func withFakeAWS(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	awsPath := filepath.Join(dir, "aws")
	err := os.WriteFile(awsPath, []byte(fakeAWSScript), 0o755)
	require.NoError(t, err)
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+":"+origPath)
}

// TestGet_DefaultProfile verifies that a request with no profile returns Version:1 process credentials.
func TestGet_DefaultProfile(t *testing.T) {
	withFakeAWS(t)

	p := New()
	inj, err := p.Get(context.Background(), credproxylib.Request{Path: "/"})
	require.NoError(t, err)
	require.NotNil(t, inj)

	var creds processCredentials
	require.NoError(t, json.Unmarshal(inj.BodyReplace, &creds))
	assert.Equal(t, 1, creds.Version)
	assert.Equal(t, "ID-default", creds.AccessKeyId)
	assert.Equal(t, "TOKEN", creds.SessionToken)
}

// TestGet_NamedProfile verifies that profile name from the request path is forwarded to the CLI.
func TestGet_NamedProfile(t *testing.T) {
	withFakeAWS(t)

	p := New()
	inj, err := p.Get(context.Background(), credproxylib.Request{Path: "/master"})
	require.NoError(t, err)

	var creds processCredentials
	require.NoError(t, json.Unmarshal(inj.BodyReplace, &creds))
	assert.Equal(t, "ID-master", creds.AccessKeyId)
}

// TestGet_PerProfileCacheIsolation verifies caches for different profiles do not bleed.
func TestGet_PerProfileCacheIsolation(t *testing.T) {
	withFakeAWS(t)

	p := New()
	injMaster, err := p.Get(context.Background(), credproxylib.Request{Path: "/master"})
	require.NoError(t, err)
	injGeneral, err := p.Get(context.Background(), credproxylib.Request{Path: "/general"})
	require.NoError(t, err)

	var cm, cg processCredentials
	require.NoError(t, json.Unmarshal(injMaster.BodyReplace, &cm))
	require.NoError(t, json.Unmarshal(injGeneral.BodyReplace, &cg))
	assert.Equal(t, "ID-master", cm.AccessKeyId)
	assert.Equal(t, "ID-general", cg.AccessKeyId)
}

// TestCache_ReusesWithinMargin verifies the second call uses cache without re-invoking aws.
func TestCache_ReusesWithinMargin(t *testing.T) {
	withFakeAWS(t)

	p := New()
	inj1, err := p.Get(context.Background(), credproxylib.Request{Path: "/master"})
	require.NoError(t, err)

	t.Setenv("PATH", "")

	inj2, err := p.Get(context.Background(), credproxylib.Request{Path: "/master"})
	require.NoError(t, err)
	assert.Equal(t, inj1.BodyReplace, inj2.BodyReplace)
}

// TestRefresh_ClearsCacheAndRefetches verifies Refresh evicts only the target profile.
func TestRefresh_ClearsCacheAndRefetches(t *testing.T) {
	withFakeAWS(t)

	p := New()
	_, err := p.Get(context.Background(), credproxylib.Request{Path: "/master"})
	require.NoError(t, err)
	p.mu.Lock()
	assert.NotNil(t, p.cache["master"])
	p.mu.Unlock()

	inj, err := p.Refresh(context.Background(), credproxylib.Request{Path: "/master"})
	require.NoError(t, err)

	var creds processCredentials
	require.NoError(t, json.Unmarshal(inj.BodyReplace, &creds))
	assert.Equal(t, "ID-master", creds.AccessKeyId)

	p.mu.Lock()
	assert.NotNil(t, p.cache["master"])
	p.mu.Unlock()
}

// TestCache_Expiry verifies an expired entry causes a re-fetch.
func TestCache_Expiry(t *testing.T) {
	withFakeAWS(t)

	p := New()
	expiredBody, _ := json.Marshal(processCredentials{Version: 1, AccessKeyId: "EXPIRED"})
	p.mu.Lock()
	p.cache["master"] = &cachedCreds{body: expiredBody, expires: time.Now().Add(-1 * time.Second)}
	p.mu.Unlock()

	inj, err := p.Get(context.Background(), credproxylib.Request{Path: "/master"})
	require.NoError(t, err)

	var creds processCredentials
	require.NoError(t, json.Unmarshal(inj.BodyReplace, &creds))
	assert.Equal(t, "ID-master", creds.AccessKeyId)
}

// TestProfileFromPath verifies path → profile name extraction.
func TestProfileFromPath(t *testing.T) {
	cases := []struct{ path, want string }{
		{"/", ""},
		{"", ""},
		{"/master", "master"},
		{"/general", "general"},
		{"/default", ""}, // "default" maps to "" (= no --profile flag)
	}
	for _, tc := range cases {
		got := profileFromPath(tc.path)
		assert.Equal(t, tc.want, got, "path=%q", tc.path)
	}
}

// TestContainerEnv verifies that ContainerEnv returns ROOST_* keys.
func TestContainerEnv(t *testing.T) {
	env := ContainerEnv("http://host.docker.internal:9000", "mytoken")
	assert.Equal(t, "mytoken", env["ROOST_AWS_TOKEN"])
	assert.Equal(t, "9000", env["ROOST_PROXY_PORT"])
}

// TestParseExpiration verifies the edge cases of parseExpiration.
func TestParseExpiration(t *testing.T) {
	assert.True(t, parseExpiration("").IsZero())
	assert.True(t, parseExpiration("not-a-date").IsZero())

	expected := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, expected, parseExpiration("2099-01-01T00:00:00Z"))
}

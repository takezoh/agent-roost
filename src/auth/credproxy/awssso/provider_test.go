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

// fakeAWSScript is a minimal shell script that acts as the "aws" binary.
// When invoked with "configure export-credentials", it prints process-format JSON.
const fakeAWSScript = `#!/bin/sh
case "$*" in
  "configure export-credentials --format process")
    echo '{"Version":1,"AccessKeyId":"FAKEID","SecretAccessKey":"FAKESECRET","SessionToken":"FAKETOKEN","Expiration":"2099-01-01T00:00:00Z"}'
    ;;
  *)
    echo "unknown args: $*" >&2
    exit 1
    ;;
esac
`

// withFakeAWS writes a fake aws binary to a temp directory and prepends it to PATH.
// It returns a cleanup function. The fake binary only handles
// "configure export-credentials --format process".
func withFakeAWS(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	awsPath := filepath.Join(dir, "aws")
	err := os.WriteFile(awsPath, []byte(fakeAWSScript), 0o755)
	require.NoError(t, err)

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+":"+origPath)
}

// processFormatJSON returns what "aws configure export-credentials --format process" outputs.
func processFormatJSON(accessKeyID, secretKey, sessionToken, expiration string) []byte {
	m := map[string]interface{}{
		"Version":         1,
		"AccessKeyId":     accessKeyID,
		"SecretAccessKey": secretKey,
		"SessionToken":    sessionToken,
		"Expiration":      expiration,
	}
	data, _ := json.Marshal(m)
	return data
}

// TestAWSSSO_ExportCredentials_Success verifies that when the "aws" command is
// available and returns valid process-format credentials, Get returns them as an
// IMDS-compatible JSON body.
func TestAWSSSO_ExportCredentials_Success(t *testing.T) {
	withFakeAWS(t)

	p := New()
	inj, err := p.Get(context.Background(), credproxylib.Request{})
	require.NoError(t, err)
	require.NotNil(t, inj)
	require.NotNil(t, inj.BodyReplace)

	var creds imdsCredentials
	require.NoError(t, json.Unmarshal(inj.BodyReplace, &creds))
	assert.Equal(t, "FAKEID", creds.AccessKeyId)
	assert.Equal(t, "FAKESECRET", creds.SecretAccessKey)
	assert.Equal(t, "FAKETOKEN", creds.Token)
}

// TestAWSSSO_Cache_ReusesWithinMargin verifies that a second call to Get within
// the refresh margin reuses the cached credentials without re-invoking the aws CLI.
func TestAWSSSO_Cache_ReusesWithinMargin(t *testing.T) {
	withFakeAWS(t)

	p := New()

	// First call populates the cache.
	inj1, err := p.Get(context.Background(), credproxylib.Request{})
	require.NoError(t, err)
	require.NotNil(t, inj1)

	// Replace PATH so that the fake aws is no longer accessible.
	// If the second Get invokes the real aws CLI or fails to find any binary,
	// it means the cache is NOT being used — which would fail the test.
	t.Setenv("PATH", "")

	// Second call should return the cached result without calling the aws CLI.
	inj2, err := p.Get(context.Background(), credproxylib.Request{})
	require.NoError(t, err)
	require.NotNil(t, inj2)

	assert.Equal(t, inj1.BodyReplace, inj2.BodyReplace)
}

// TestAWSSSO_Refresh_ClearsCacheAndRefetches verifies that Refresh clears the
// cache and re-invokes the aws CLI.
func TestAWSSSO_Refresh_ClearsCacheAndRefetches(t *testing.T) {
	withFakeAWS(t)

	p := New()

	// Populate the cache via Get.
	_, err := p.Get(context.Background(), credproxylib.Request{})
	require.NoError(t, err)
	require.NotNil(t, p.cache, "cache should be populated after Get")

	// Refresh must clear the cache and re-fetch.
	inj, err := p.Refresh(context.Background(), credproxylib.Request{})
	require.NoError(t, err)
	require.NotNil(t, inj)

	var creds imdsCredentials
	require.NoError(t, json.Unmarshal(inj.BodyReplace, &creds))
	assert.Equal(t, "FAKEID", creds.AccessKeyId)

	// Cache should be repopulated after Refresh.
	p.mu.Lock()
	cached := p.cache
	p.mu.Unlock()
	assert.NotNil(t, cached)
}

// TestAWSSSO_Cache_Expiry verifies that an expired cache entry causes a fresh
// fetch on the next Get call.
func TestAWSSSO_Cache_Expiry(t *testing.T) {
	withFakeAWS(t)

	p := New()

	// Manually inject an already-expired cache entry.
	expiredBody, _ := json.Marshal(imdsCredentials{
		AccessKeyId:     "EXPIRED",
		SecretAccessKey: "EXPIRED",
		Token:           "EXPIRED",
	})
	p.mu.Lock()
	p.cache = &cachedCreds{
		body:    expiredBody,
		expires: time.Now().Add(-1 * time.Second), // already expired
	}
	p.mu.Unlock()

	// Get should detect expiry and re-fetch from the fake aws CLI.
	inj, err := p.Get(context.Background(), credproxylib.Request{})
	require.NoError(t, err)
	require.NotNil(t, inj)

	var creds imdsCredentials
	require.NoError(t, json.Unmarshal(inj.BodyReplace, &creds))
	// Should get fresh credentials from the fake aws binary, not the expired ones.
	assert.Equal(t, "FAKEID", creds.AccessKeyId)
}

// TestParseExpiration verifies the edge cases of the internal parseExpiration helper.
func TestParseExpiration(t *testing.T) {
	assert.True(t, parseExpiration("").IsZero())
	assert.True(t, parseExpiration("not-a-date").IsZero())

	expected := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, expected, parseExpiration("2099-01-01T00:00:00Z"))
}

// TestExportCredentials_ProcessFormatParsing verifies that processFormatJSON
// round-trips correctly through the IMDS credentials structure.
func TestExportCredentials_ProcessFormatParsing(t *testing.T) {
	data := processFormatJSON("AID", "SECRET", "TOKEN", "2099-01-01T00:00:00Z")
	var raw struct {
		AccessKeyId     string `json:"AccessKeyId"`
		SecretAccessKey string `json:"SecretAccessKey"`
		SessionToken    string `json:"SessionToken"`
		Expiration      string `json:"Expiration"`
	}
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.Equal(t, "AID", raw.AccessKeyId)
	assert.Equal(t, "SECRET", raw.SecretAccessKey)
	assert.Equal(t, "TOKEN", raw.SessionToken)
	assert.Equal(t, "2099-01-01T00:00:00Z", raw.Expiration)
}

package anthropicoauth

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	credproxylib "github.com/takezoh/credproxy/pkg/credproxy"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeCreds(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, ".credentials.json")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestProvider_Get_ReadsAccessToken(t *testing.T) {
	dir := t.TempDir()
	path := writeCreds(t, dir, `{"claudeAiOauth":{"accessToken":"sk-ant-foo","expiresAt":1777115495508}}`)

	p := NewWithPath(path)
	inj, err := p.Get(context.Background(), credproxylib.Request{})
	require.NoError(t, err)
	require.NotNil(t, inj)
	assert.Equal(t, "Bearer sk-ant-foo", inj.Headers["Authorization"])
	assert.Equal(t, time.UnixMilli(1777115495508), inj.ExpiresAt)
}

func TestProvider_Refresh_RereadsFile(t *testing.T) {
	dir := t.TempDir()
	path := writeCreds(t, dir, `{"claudeAiOauth":{"accessToken":"old"}}`)

	p := NewWithPath(path)
	inj, err := p.Get(context.Background(), credproxylib.Request{})
	require.NoError(t, err)
	assert.Equal(t, "Bearer old", inj.Headers["Authorization"])

	require.NoError(t, os.WriteFile(path, []byte(`{"claudeAiOauth":{"accessToken":"new"}}`), 0o600))

	inj, err = p.Refresh(context.Background(), credproxylib.Request{})
	require.NoError(t, err)
	assert.Equal(t, "Bearer new", inj.Headers["Authorization"])
}

func TestProvider_Get_MissingFile(t *testing.T) {
	p := NewWithPath(filepath.Join(t.TempDir(), "does-not-exist.json"))
	inj, err := p.Get(context.Background(), credproxylib.Request{})
	assert.Error(t, err)
	assert.Nil(t, inj)
}

func TestProvider_Get_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := writeCreds(t, dir, `not json`)

	p := NewWithPath(path)
	_, err := p.Get(context.Background(), credproxylib.Request{})
	assert.Error(t, err)
}

func TestProvider_Get_NoAccessToken(t *testing.T) {
	dir := t.TempDir()
	path := writeCreds(t, dir, `{"claudeAiOauth":{}}`)

	p := NewWithPath(path)
	_, err := p.Get(context.Background(), credproxylib.Request{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no claudeAiOauth.accessToken")
}

func TestNew_DefaultsToHomeClaudeCredentials(t *testing.T) {
	p := New()
	home, _ := os.UserHomeDir()
	assert.Equal(t, filepath.Join(home, ".claude", ".credentials.json"), p.path)
}

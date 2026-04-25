package awssso

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderConfig_MultipleProfiles(t *testing.T) {
	var sb strings.Builder
	err := RenderConfig(&sb, []string{"default", "master", "general"}, "/opt/roost/aws-creds")
	require.NoError(t, err)

	out := sb.String()
	assert.Contains(t, out, "[default]\ncredential_process = /opt/roost/aws-creds default")
	assert.Contains(t, out, "[profile master]\ncredential_process = /opt/roost/aws-creds master")
	assert.Contains(t, out, "[profile general]\ncredential_process = /opt/roost/aws-creds general")
	// "default" must not use the "profile " prefix
	assert.NotContains(t, out, "[profile default]")
}

func TestRenderConfig_Empty(t *testing.T) {
	var sb strings.Builder
	err := RenderConfig(&sb, []string{}, "/opt/roost/aws-creds")
	require.NoError(t, err)
	assert.Empty(t, sb.String())
}

func TestRenderConfig_InvalidProfileName(t *testing.T) {
	cases := []string{
		"",
		"has space",
		"has\ttab",
		"has]bracket",
		"has\nnewline",
		"has'quote",
	}
	for _, name := range cases {
		var sb strings.Builder
		err := RenderConfig(&sb, []string{name}, "/opt/roost/aws-creds")
		assert.Error(t, err, "expected error for profile %q", name)
	}
}

func TestWriteHelperScript(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/aws-creds"
	err := WriteHelperScript(path)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "ROOST_AWS_TOKEN")
	assert.Contains(t, string(data), "ROOST_PROXY_PORT")
	assert.Contains(t, string(data), "/aws-credentials/$1")

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode())
}

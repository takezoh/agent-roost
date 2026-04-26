package github

import (
	"net"
	"os"
)

// RoutePath is the proxy path prefix served by the GitHub credential handler.
const RoutePath = "/git-credentials"

// containerHelperPath is where the git credential helper script is mounted inside the container.
const containerHelperPath = "/opt/roost/git-credential-roost"

// containerGitconfigPath is the synthetic git config file path inside the container.
const containerGitconfigPath = "/opt/roost/gitconfig"

// helperScript is the container-side git credential helper.
// git calls it with "get", "store", or "erase" as $1.
// Only "get" is handled; the server returns "username=\npassword=<token>\n".
const helperScript = `#!/bin/sh
case "$1" in
    get)
        exec curl -fsSL \
            --data-binary @- \
            -H "Authorization: Bearer $ROOST_GIT_TOKEN" \
            "http://host.docker.internal:${ROOST_PROXY_PORT}/git-credentials"
        ;;
    store|erase) : ;;
esac
`

// gitconfigSnippet is the synthetic gitconfig mounted into the container.
// GIT_CONFIG_GLOBAL points to this file so the user's ~/.gitconfig is not affected.
const gitconfigSnippet = `[credential "https://github.com"]
	helper = /opt/roost/git-credential-roost
[credential "https://gist.github.com"]
	helper = /opt/roost/git-credential-roost
`

func writeHelperScript(path string) error {
	return os.WriteFile(path, []byte(helperScript), 0o755)
}

func writeGitconfig(path string) error {
	return os.WriteFile(path, []byte(gitconfigSnippet), 0o644)
}

// containerEnv returns the env vars a container must set to reach the credential proxy.
func containerEnv(proxyAddr, token string) map[string]string {
	_, port, _ := net.SplitHostPort(proxyAddr)
	return map[string]string{
		"ROOST_GIT_TOKEN":   token,
		"ROOST_PROXY_PORT":  port,
		"GIT_CONFIG_GLOBAL": containerGitconfigPath,
	}
}

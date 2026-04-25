package awssso

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// helperScript is the container-side credential_process helper.
// It receives the profile name as $1 and calls back to the roost proxy.
const helperScript = `#!/bin/sh
exec curl -fsSL -H "Authorization: Bearer $ROOST_AWS_TOKEN" \
  "http://host.docker.internal:${ROOST_PROXY_PORT}/aws-credentials/$1"
`

// WriteHelperScript materializes the helper script at hostPath with mode 0o755.
func WriteHelperScript(hostPath string) error {
	return os.WriteFile(hostPath, []byte(helperScript), 0o755)
}

// RenderConfig writes a synthetic ~/.aws/config to w.
// Each name in profiles becomes a [profile <name>] section with credential_process
// pointing to scriptPath. If "default" is listed, a [default] section is emitted
// (no "profile " prefix per the AWS config spec).
// scriptPath is the in-container path to the helper script (e.g. /opt/roost/aws-creds).
func RenderConfig(w io.Writer, profiles []string, scriptPath string) error {
	for _, name := range profiles {
		if err := validateProfileName(name); err != nil {
			return err
		}
		var section string
		if name == "default" {
			section = "[default]"
		} else {
			section = "[profile " + name + "]"
		}
		if _, err := fmt.Fprintf(w, "%s\ncredential_process = %s %s\n\n", section, scriptPath, name); err != nil {
			return err
		}
	}
	return nil
}

// validateProfileName rejects names containing characters that are invalid in
// an ini section header or unsafe as a shell argument positional to the helper.
func validateProfileName(name string) error {
	if name == "" {
		return fmt.Errorf("awssso: profile name must not be empty")
	}
	for _, ch := range name {
		if ch == ']' || ch == '\n' || ch == '\r' || ch == '\x00' {
			return fmt.Errorf("awssso: profile name %q contains invalid character", name)
		}
	}
	// Guard against shell word-splitting / metachar injection via $1 in the script.
	if strings.ContainsAny(name, " \t\\'\"") || strings.ContainsRune(name, '`') {
		return fmt.Errorf("awssso: profile name %q contains shell-unsafe character", name)
	}
	return nil
}

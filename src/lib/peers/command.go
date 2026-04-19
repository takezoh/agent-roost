package peers

import (
	"github.com/takezoh/agent-roost/cli"
)

func init() {
	cli.Register("peers-mcp", "roost-peers MCP server (stdio)", Run)
}

// Run starts the peers MCP server.
func Run(args []string) error {
	return runMCPServer()
}

package send

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/lib"
	"github.com/takezoh/agent-roost/proto"
)

func init() {
	lib.Register("send", "Send an IPC command to the daemon", Run)
}

// Run parses args as: <command-name> [key=value ...]
func Run(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: roost send <command> [key=value ...]")
		os.Exit(1)
	}
	cmdName := args[0]
	cmdArgs := parseArgs(args[1:])

	cmd, err := proto.BuildCommand(cmdName, cmdArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "roost send: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "roost send: %v\n", err)
		os.Exit(1)
	}
	sockPath := filepath.Join(cfg.ResolveDataDir(), "roost.sock")
	client, err := proto.Dial(sockPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "roost send: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.Send(ctx, cmd); err != nil {
		fmt.Fprintf(os.Stderr, "roost send: %v\n", err)
		os.Exit(1)
	}
}

func parseArgs(raw []string) map[string]string {
	m := make(map[string]string, len(raw))
	for _, s := range raw {
		k, v, _ := strings.Cut(s, "=")
		m[k] = v
	}
	return m
}

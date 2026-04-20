package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/features"
	"github.com/takezoh/agent-roost/lib/git"
	"github.com/takezoh/agent-roost/lib/openurl"
	"github.com/takezoh/agent-roost/logger"
	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/tools"
	"github.com/takezoh/agent-roost/tui"
	"github.com/takezoh/agent-roost/tui/glyphs"
)

type tuiBootstrapOpts struct {
	Subscribe    bool
	AllowOffline bool
}

// tuiBootstrap loads config, applies theme, initialises glyphs, and dials the IPC socket.
// If AllowOffline is true and Dial fails, returns (cfg, nil, nil).
// If Subscribe is true and Dial succeeds, calls client.Subscribe().
// Caller must defer client.Close() when client is non-nil.
func tuiBootstrap(opts tuiBootstrapOpts) (*config.Config, *proto.Client, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, nil, err
	}
	initThemes(cfg.Theme)
	initGlyphs()
	sockPath := filepath.Join(cfg.ResolveDataDir(), "roost.sock")

	client, err := proto.Dial(sockPath)
	if err != nil {
		if opts.AllowOffline {
			return cfg, nil, nil
		}
		return nil, nil, fmt.Errorf("connect: %w", err)
	}

	if opts.Subscribe {
		_ = client.Subscribe()
	}
	return cfg, client, nil
}

// initThemes loads user themes from ~/.roost/themes/ then selects the active
// theme from ROOST_THEME env (highest priority), the config value, or "default".
func initThemes(cfgTheme string) {
	if home, err := os.UserHomeDir(); err == nil {
		tui.LoadThemesFromDir(filepath.Join(home, ".roost", "themes"))
	}
	name := cfgTheme
	if env := os.Getenv("ROOST_THEME"); env != "" {
		name = env
	}
	tui.ApplyTheme(name)
}

// initGlyphs loads the optional user glyph override and applies the
// ROOST_GLYPHS environment variable (default: "nerd").
func initGlyphs() {
	home, err := os.UserHomeDir()
	if err == nil {
		if err := glyphs.Load(filepath.Join(home, ".roost", "glyphs.json")); err != nil {
			slog.Warn("glyphs: load error", "err", err)
		}
	}
	if name := os.Getenv("ROOST_GLYPHS"); name != "" {
		glyphs.Use(name)
	}
}

func runMainTUI() error {
	_, client, err := tuiBootstrap(tuiBootstrapOpts{Subscribe: true, AllowOffline: true})
	if err != nil {
		return err
	}
	if client != nil {
		defer client.Close()
	}
	model := tui.NewMainModel(client)
	if _, err := tea.NewProgram(model).Run(); err != nil {
		return fmt.Errorf("main: %w", err)
	}
	return nil
}

func runLogViewer() error {
	_, client, err := tuiBootstrap(tuiBootstrapOpts{Subscribe: true, AllowOffline: true})
	if err != nil {
		return err
	}
	if client != nil {
		defer client.Close()
	}
	tui.SetOpenProject(openurl.Open)
	model := tui.NewLogModel(logger.LogFilePath(), client)
	if _, err := tea.NewProgram(model).Run(); err != nil {
		return fmt.Errorf("log: %w", err)
	}
	return nil
}

func runSessionList() error {
	cfg, client, err := tuiBootstrap(tuiBootstrapOpts{Subscribe: true, AllowOffline: false})
	if err != nil {
		return err
	}
	defer client.Close()
	model := tui.NewModel(client, cfg)
	if _, err := tea.NewProgram(model).Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}

func runPalette(args []string) error { //nolint:funlen
	slog.Info("palette start", "args", args)
	cfg, client, err := tuiBootstrap(tuiBootstrapOpts{Subscribe: false, AllowOffline: false})
	if err != nil {
		slog.Error("palette bootstrap failed", "err", err)
		return err
	}
	slog.Info("palette connected")
	defer client.Close()

	var toolName string
	prefill := make(map[string]string)
	for _, a := range args {
		if strings.HasPrefix(a, "--tool=") {
			toolName = strings.TrimPrefix(a, "--tool=")
		} else if strings.HasPrefix(a, "--arg=") {
			kv := strings.TrimPrefix(a, "--arg=")
			if parts := strings.SplitN(kv, "=", 2); len(parts) == 2 {
				prefill[parts[0]] = parts[1]
			}
		}
	}

	if toolName == "push-driver" {
		_, activeID, _, _, err := client.ListSessions()
		if err != nil || activeID == "" {
			fmt.Fprintln(os.Stderr, "no active session")
			time.Sleep(700 * time.Millisecond)
			return nil
		}
		prefill["session_id"] = activeID
	}

	feats := features.FromConfig(cfg.Features.Enabled, features.All())
	reg := tools.DefaultRegistry(feats)
	roots := make([]string, len(cfg.Projects.ProjectRoots))
	for i, r := range cfg.Projects.ProjectRoots {
		roots[i] = config.ExpandPath(r)
	}
	ctx := &tools.ToolContext{
		Client: client,
		Config: tools.ToolConfig{
			DefaultCommand: cfg.Session.DefaultCommand,
			Commands:       cfg.Session.Commands,
			PushCommands:   cfg.Session.PushCommands,
			Projects:       cfg.ListProjects(),
			ProjectRoots:   roots,
		},
		Args:         prefill,
		IsGitProject: git.IsRepo,
	}

	model := tui.NewPaletteModel(reg, ctx, toolName)
	if _, err := tea.NewProgram(model).Run(); err != nil {
		return fmt.Errorf("palette: %w", err)
	}
	return nil
}

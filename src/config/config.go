package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	DataDir       string                    `toml:"data_dir"`
	Theme         string                    `toml:"theme"`
	Log           LogConfig                 `toml:"log"`
	Tmux          TmuxConfig                `toml:"tmux"`
	Monitor       MonitorConfig             `toml:"monitor"`
	Session       SessionConfig             `toml:"session"`
	Projects      ProjectsConfig            `toml:"projects"`
	Driver        CommonDriverConfig        `toml:"driver"`
	Drivers       map[string]map[string]any `toml:"drivers"`
	Features      FeaturesConfig            `toml:"features"`
	Notifications NotificationsConfig       `toml:"notifications"`
	Sandbox       SandboxConfig             `toml:"sandbox"`
}

// SandboxConfig controls how agent processes are isolated.
// mode = "direct" runs agents with no extra sandboxing (default).
// mode = "docker" runs each project in a long-lived Docker container.
type SandboxConfig struct {
	Mode   string       `toml:"mode"`
	Docker DockerConfig `toml:"docker"`
}

// DockerConfig holds Docker-specific sandbox parameters.
type DockerConfig struct {
	Image       string            `toml:"image"`
	Network     string            `toml:"network"`
	ExtraArgs   []string          `toml:"extra_args"`
	ExtraMounts []string          `toml:"extra_mounts"` // "host:guest[:mode]" appended to docker run -v
	Env         map[string]string `toml:"env"`          // fixed env passed via -e
	ForwardEnv  []string          `toml:"forward_env"`  // host env vars to pass through if set
}

// CommonDriverConfig holds settings that apply to all drivers.
type CommonDriverConfig struct {
	SummarizeCommand string `toml:"summarize_command"`
	Pager            string `toml:"pager"`
}

// FeaturesConfig holds the runtime feature-flag table from the TOML config.
// Each key in Enabled is a [features.Flag] identifier; true enables the flag.
type FeaturesConfig struct {
	Enabled map[string]bool `toml:"enabled"`
}

// LogConfig controls slog handler verbosity. Level values: "debug", "info",
// "warn", "error". Unknown / empty values fall back to info in logger.Init.
type LogConfig struct {
	Level string `toml:"level"`
}

type TmuxConfig struct {
	SessionName         string `toml:"session_name"`
	Prefix              string `toml:"prefix"`
	PaneRatioHorizontal int    `toml:"pane_ratio_horizontal"`
	PaneRatioVertical   int    `toml:"pane_ratio_vertical"`
}

type MonitorConfig struct {
	PollIntervalMs     int `toml:"poll_interval_ms"`
	FastPollIntervalMs int `toml:"fast_poll_interval_ms"`
	IdleThresholdSec   int `toml:"idle_threshold_sec"`
}

type SessionConfig struct {
	AutoName       bool              `toml:"auto_name"`
	DefaultCommand string            `toml:"default_command"`
	Commands       []string          `toml:"commands"`
	PushCommands   []string          `toml:"push_commands"`
	Aliases        map[string]string `toml:"aliases"`
}

// ResolveAlias expands a command string through the alias map. Unknown
// commands are returned unchanged. Aliases are matched against the entire
// trimmed input string, not parsed tokens, so "clw" maps but "clw foo" does
// not (matching shell alias semantics where the alias name is the first word).
func (s SessionConfig) ResolveAlias(command string) string {
	command = strings.TrimSpace(command)
	if expanded, ok := s.Aliases[command]; ok {
		return expanded
	}
	return command
}

type ProjectsConfig struct {
	ProjectRoots []string `toml:"project_roots"`
	ProjectPaths []string `toml:"project_paths"`
}

func LoadFrom(path string) (*Config, error) {
	cfg := DefaultConfig()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil
	}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, err
	}
	if err := cfg.Notifications.Validate(); err != nil {
		return nil, err
	}
	cfg.Driver.Pager = resolvePager(cfg.Driver.Pager)
	return cfg, nil
}

// resolvePager returns the effective pager command. Priority:
//  1. value from config (if non-empty)
//  2. $PAGER environment variable
//  3. "less" as the universal fallback
func resolvePager(configured string) string {
	if configured != "" {
		return configured
	}
	if p := os.Getenv("PAGER"); p != "" {
		return p
	}
	return "less"
}

func Load() (*Config, error) {
	return LoadFrom(filepath.Join(EnsureConfigDir(), "settings.toml"))
}

func DefaultConfig() *Config {
	return &Config{
		Theme: "default",
		Log:   LogConfig{Level: "info"},
		Tmux: TmuxConfig{
			SessionName:         "roost",
			Prefix:              "C-b",
			PaneRatioHorizontal: 75,
			PaneRatioVertical:   75,
		},
		Monitor: MonitorConfig{
			PollIntervalMs:     1000,
			FastPollIntervalMs: 100,
			IdleThresholdSec:   30,
		},
		Session: SessionConfig{
			AutoName:       true,
			DefaultCommand: "shell",
			Commands:       []string{"shell"},
			PushCommands:   []string{"shell"},
		},
		Projects: ProjectsConfig{},
		Sandbox:  SandboxConfig{Mode: "direct"},
	}
}

func ConfigDirPath() string {
	return filepath.Join(ExpandPath("~"), ".roost")
}

func EnsureConfigDir() string {
	dir := ConfigDirPath()
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func (c *Config) ListProjects() []string {
	var projects []string
	for _, root := range c.Projects.ProjectRoots {
		root = ExpandPath(root)
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				projects = append(projects, filepath.Join(root, e.Name()))
			}
		}
	}
	for _, p := range c.Projects.ProjectPaths {
		p = ExpandPath(p)
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			projects = append(projects, p)
		}
	}
	return projects
}

func (c *Config) ResolveDataDir() string {
	if c.DataDir != "" {
		return ExpandPath(c.DataDir)
	}
	return ConfigDirPath()
}

func ExpandPath(p string) string {
	if strings.HasPrefix(p, "~") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[1:])
	}
	return p
}

package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	DataDir  string         `toml:"data_dir"`
	Tmux     TmuxConfig     `toml:"tmux"`
	Monitor  MonitorConfig  `toml:"monitor"`
	Session  SessionConfig  `toml:"session"`
	Projects ProjectsConfig `toml:"projects"`
}

type TmuxConfig struct {
	SessionName         string `toml:"session_name"`
	Prefix              string `toml:"prefix"`
	PaneRatioHorizontal int    `toml:"pane_ratio_horizontal"`
	PaneRatioVertical   int    `toml:"pane_ratio_vertical"`
}

type MonitorConfig struct {
	PollIntervalMs   int `toml:"poll_interval_ms"`
	IdleThresholdSec int `toml:"idle_threshold_sec"`
}

type SessionConfig struct {
	AutoName       bool     `toml:"auto_name"`
	DefaultCommand string   `toml:"default_command"`
	Commands       []string `toml:"commands"`
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
	return cfg, nil
}

func Load() (*Config, error) {
	return LoadFrom(filepath.Join(EnsureConfigDir(), "config.toml"))
}

func DefaultConfig() *Config {
	return &Config{
		Tmux: TmuxConfig{
			SessionName:         "roost",
			Prefix:              "C-b",
			PaneRatioHorizontal: 75,
			PaneRatioVertical:   70,
		},
		Monitor: MonitorConfig{
			PollIntervalMs:   1000,
			IdleThresholdSec: 30,
		},
		Session: SessionConfig{
			AutoName:       true,
			DefaultCommand: "claude",
			Commands:       []string{"claude", "gemini", "codex"},
		},
		Projects: ProjectsConfig{
			ProjectRoots: []string{"~/dev", "~/work"},
		},
	}
}

func ConfigDirPath() string {
	return filepath.Join(ExpandPath("~"), ".config", "roost")
}

func EnsureConfigDir() string {
	dir := ConfigDirPath()
	os.MkdirAll(dir, 0o755)
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

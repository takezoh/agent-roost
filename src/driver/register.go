package driver

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/takezoh/agent-roost/state"
)

// RegisterOptions carries startup parameters for all built-in drivers.
// DriverConfigs is the raw [drivers.*] map from settings.toml — each
// driver decodes its own section via decodeConfig.
type RegisterOptions struct {
	Home          string
	EventLogDir   string
	IdleThreshold time.Duration
	DriverConfigs map[string]map[string]any
}

// RegisterDefaults wires the built-in driver set into the global
// state registry. Idempotent — repeated calls are no-ops so test
// binaries that import multiple sub-packages don't double-register.
//
// The "shell" driver is intentionally NOT registered here because its
// display name must reflect the shell tmux will actually spawn
// (tmux default-shell option). Call RegisterShellDriver separately,
// after tmux is running.
func RegisterDefaults(opts RegisterOptions) {
	registerOnce.Do(func() {
		claudeOpts := decodeConfig[ClaudeOptions](opts.DriverConfigs[ClaudeDriverName])
		state.Register(NewClaudeDriver(opts.Home, opts.EventLogDir, claudeOpts))
		state.Register(NewCodexDriver(opts.EventLogDir))
		state.Register(NewGeminiDriver(opts.EventLogDir))
		state.Register(NewGenericDriver("", opts.IdleThreshold))
	})
}

// RegisterShellDriver registers the "shell" generic driver with the
// given display name. Must be called after tmux is running so the
// display name matches the shell tmux will actually spawn.
// Idempotent — repeated calls (warm/cold paths) are no-ops.
var registerOnce sync.Once
var shellOnce sync.Once

func RegisterShellDriver(threshold time.Duration, displayName string) {
	if displayName == "" {
		displayName = "shell"
	}
	shellOnce.Do(func() {
		state.Register(NewGenericDriver("shell", threshold).WithDisplayName(displayName))
	})
}

// ParseClaudeOptions decodes the driver config section keyed by
// ClaudeDriverName into a ClaudeOptions value. Exported so the runtime coordinator can read
// it without duplicating the JSON round-trip logic.
func ParseClaudeOptions(raw map[string]any) ClaudeOptions {
	return decodeConfig[ClaudeOptions](raw)
}

// decodeConfig converts a raw map (from TOML) into a typed config
// struct via JSON round-trip. Same pattern as RegisterTabRenderer's
// json.RawMessage unmarshaling.
func decodeConfig[T any](raw map[string]any) T {
	var cfg T
	if len(raw) == 0 {
		return cfg
	}
	b, _ := json.Marshal(raw)
	if err := json.Unmarshal(b, &cfg); err != nil {
		slog.Debug("driver: decode config", "err", err)
	}
	return cfg
}

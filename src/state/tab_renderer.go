package state

import (
	"encoding/json"
	"log/slog"
)

// TabRenderer is the interface TUI uses to render tab content without
// knowing the driver-specific format. Drivers register a factory via
// RegisterTabRenderer at init time; the TUI creates renderers via
// NewTabRenderer using the LogTab's Kind and RendererCfg.
type TabRenderer interface {
	Append(data []byte) string
	Reset()
}

// ShowThinkingToggler is an optional interface a TabRenderer may
// implement to support toggling visibility of "thinking" blocks.
type ShowThinkingToggler interface {
	SetShowThinking(bool)
}

var rendererFactories = map[TabKind]func(json.RawMessage) TabRenderer{}

// RegisterTabRenderer registers a typed factory for a TabKind. The
// generic type parameter C is the driver-specific config struct that
// is serialized into LogTab.RendererCfg by the driver. Same pattern
// as worker.Submit[In, Out].
func RegisterTabRenderer[C any](kind TabKind, factory func(C) TabRenderer) {
	rendererFactories[kind] = func(raw json.RawMessage) TabRenderer {
		var cfg C
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &cfg); err != nil {
				slog.Debug("tab_renderer: unmarshal config", "kind", kind, "err", err)
			}
		}
		return factory(cfg)
	}
}

// HasTabRenderer reports whether a factory is registered for the kind.
func HasTabRenderer(kind TabKind) bool {
	_, ok := rendererFactories[kind]
	return ok
}

// NewTabRenderer creates a TabRenderer for the given kind using the
// registered factory. Returns nil if no factory is registered.
func NewTabRenderer(kind TabKind, cfg json.RawMessage) TabRenderer {
	f, ok := rendererFactories[kind]
	if !ok {
		return nil
	}
	return f(cfg)
}

package state

import (
	"encoding/json"
	"time"
)

// View is the complete TUI payload for one session, produced by its
// Driver.Step. The runtime serializes it into a proto.SessionInfo for
// IPC; the TUI renders the fields generically without any driver-name
// branching.
//
// Step is a pure function — drivers must build View from already-known
// state without performing I/O. Heavy work (file parsing, branch
// detection) happens in worker jobs and feeds back via DEvJobResult.
//
// JSON tags are present so the proto layer can ship View values
// directly across the wire without a parallel type hierarchy.
type View struct {
	Card            Card       `json:"card"`
	LogTabs         []LogTab   `json:"log_tabs,omitempty"`
	InfoExtras      []InfoLine `json:"info_extras,omitempty"`
	SuppressInfo    bool       `json:"suppress_info,omitempty"`
	StatusLine      string     `json:"status_line,omitempty"`
	Status          Status     `json:"status,omitempty"`
	StatusChangedAt time.Time  `json:"status_changed_at,omitempty"`
}

// Card is the driver-specific portion of the session list card. Generic
// fields (state, project, created_at) are rendered by the TUI from
// proto.SessionInfo. The driver fills its own Title / Subtitle / Tags /
// Indicators.
type Card struct {
	Title       string   `json:"title,omitempty"`
	Subtitle    string   `json:"subtitle,omitempty"`
	Tags        []Tag    `json:"tags,omitempty"`
	Indicators  []string `json:"indicators,omitempty"`
	BorderTitle string   `json:"border_title,omitempty"`
	BorderBadge string   `json:"border_badge,omitempty"`
}

// Tag is a colored chip rendered in the session card. The driver picks
// the color directly so adding a tag type is a single-file change.
type Tag struct {
	Text       string `json:"text"`
	Foreground string `json:"fg,omitempty"`
	Background string `json:"bg,omitempty"`
}

// LogTab declares an additional log tab the driver wants the TUI to
// display. Path is an absolute file path the TUI tails (or, in the
// push model, the runtime watches and broadcasts diffs for).
type LogTab struct {
	Label       string          `json:"label"`
	Path        string          `json:"path"`
	Kind        TabKind         `json:"kind"`
	RendererCfg json.RawMessage `json:"renderer_cfg,omitempty"`
}

// TabKind selects the renderer the TUI applies to a tab's contents.
// Drivers define their own TabKind constants and register a
// TabRenderer factory for each via RegisterTabRenderer.
type TabKind string

// TabKindText is the built-in plain-text kind. Drivers may use it for
// tabs that need no special rendering (e.g. event logs).
const TabKindText TabKind = "text"

// InfoLine is one entry in the INFO tab body. The driver returns the
// driver-specific lines via View.InfoExtras; the TUI prepends a
// generic header (ID / Project / Command / State / Created) on its
// own from proto.SessionInfo.
type InfoLine struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

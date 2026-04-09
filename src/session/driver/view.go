package driver

// SessionView is the complete TUI payload for one session, produced by
// its Driver. SessionService never inspects these fields; the TUI
// renders them as-is.
//
// View() is a pure getter — drivers must build SessionView from already
// cached internal state without performing I/O or detection. Heavy work
// (transcript meta refresh, branch detection) belongs in Tick().
type SessionView struct {
	Card         CardView
	LogTabs      []LogTab   // additional log tabs (TRANSCRIPT, EVENTS, ...). LOG tab is always appended by TUI.
	InfoExtras   []InfoLine // driver-specific INFO additions; nil = TUI shows generic header only.
	SuppressInfo bool       // true = hide INFO tab entirely (driver opt-out).
	StatusLine   string     // tmux status-left content for the active session
}

// CardView is the driver-specific portion of the session list card.
// Generic fields (state, project, created_at) are rendered by the TUI
// from SessionInfo. The driver fills its own Title / Subtitle / Tags /
// Indicators.
type CardView struct {
	Title      string   // primary line (e.g. transcript title)
	Subtitle   string   // secondary line (e.g. last user prompt)
	Tags       []Tag    // identity-style chips (command tag, branch tag, ...)
	Indicators []string // state-style chips (▸ tool, N subs, ...)
}

// Tag is a colored chip rendered in the session card. The driver decides
// the color directly. session.Tag and tui.Tag are intentionally separate
// types so driver/ does not import session/ or tui/.
type Tag struct {
	Text       string
	Foreground string // hex "#RRGGBB"; empty falls back to TUI default
	Background string
}

// LogTab declares an additional log tab the driver wants the TUI to
// display. Path must be an absolute file path that the TUI tails.
type LogTab struct {
	Label string
	Path  string
	Kind  TabKind
}

// TabKind selects the renderer the TUI applies to a tab's contents.
// The set is intentionally closed: a new kind requires both a driver
// emitting it and a TUI renderer that knows how to display it.
type TabKind string

const (
	TabKindText       TabKind = "text"       // raw text, line-by-line
	TabKindTranscript TabKind = "transcript" // Claude JSONL transcript renderer
)

// InfoLine is one entry in the INFO tab. The driver returns the
// driver-specific lines via SessionView.InfoExtras; the TUI prepends
// the generic header (ID / Project / Command / State / Created) on its
// own from SessionInfo.
type InfoLine struct {
	Label string
	Value string
}

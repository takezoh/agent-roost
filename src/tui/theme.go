package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/takezoh/agent-roost/state"
)

// Theme holds the colors and rendering toggles used across every TUI screen.
// Switch the active theme via ApplyTheme(name).
type Theme struct {
	Primary color.Color // titles, accents
	Accent  color.Color // tags, secondary highlights
	Fg      color.Color // default text on cards/panels
	Muted   color.Color // secondary info (last prompt)
	Dim     color.Color // help, borders, background noise
	SelBg   color.Color // selected row background (default mode)
	SelFg   color.Color // selected row foreground
	TagFg   color.Color // text color on tag chips (must contrast with tag bg)

	Running color.Color
	Waiting color.Color
	Idle    color.Color
	Stopped color.Color
	Pending color.Color

	Warn  color.Color
	Error color.Color

	// RunningGradientA/B are the two anchor colors for the spinner and
	// progress-bar gradient while a session is in the Running state.
	RunningGradientA color.Color
	RunningGradientB color.Color

	// Minimal switches the layout to a borderless, bar-marked card style
	// (no top/bottom borders, colored left bar on selection) and renders
	// tags as prefix-symbol + colored text instead of background chips.
	Minimal bool
}

func newBaseTheme() Theme {
	return Theme{
		Primary: lipgloss.Color("#7D56F4"),
		Accent:  lipgloss.Color("#7D56F4"),
		Fg:      lipgloss.Color("#EBDBB2"),
		Muted:   lipgloss.Color("#888888"),
		Dim:     lipgloss.Color("#626262"),
		SelBg:   lipgloss.Color("#3C3836"),
		SelFg:   lipgloss.Color("#EBDBB2"),
		TagFg:   lipgloss.Color("#1D2021"),

		Running: lipgloss.Color("#00ff00"),
		Waiting: lipgloss.Color("#ffff00"),
		Idle:    lipgloss.Color("#888888"),
		Stopped: lipgloss.Color("#ff0000"),
		Pending: lipgloss.Color("#ff8800"),

		Warn:  lipgloss.Color("#ffff00"),
		Error: lipgloss.Color("#ff0000"),

		RunningGradientA: lipgloss.Color("#7D56F4"),
		RunningGradientB: lipgloss.Color("#00ff80"),
	}
}

// Themes is the registry of selectable themes. Populated at init time by
// theme_loader.go from embedded JSON; users can extend it via
// ~/.roost/themes/*.json or RegisterTheme.
var Themes = map[string]Theme{}

// Active is the currently applied theme. Set by ApplyTheme; theme_loader's
// init() populates Themes and calls ApplyTheme("default") before any view
// code runs.
var Active Theme

// Pre-built styles. Reassigned by ApplyTheme so views can keep referencing
// these package-level vars without rebuilding per frame.
var (
	// Header / chrome
	titleStyle    lipgloss.Style
	badgeStyle    lipgloss.Style
	sectionStyle  lipgloss.Style
	projectStyle  lipgloss.Style
	selectedStyle lipgloss.Style
	helpKeyStyle  lipgloss.Style
	mutedStyle    lipgloss.Style

	// Session state
	runningStyle lipgloss.Style
	waitingStyle lipgloss.Style
	idleStyle    lipgloss.Style
	stoppedStyle lipgloss.Style
	pendingStyle lipgloss.Style

	// Log screen
	activeTabStyle   lipgloss.Style
	inactiveTabStyle lipgloss.Style
	logWarnStyle     lipgloss.Style
	logErrorStyle    lipgloss.Style
	logDebugStyle    lipgloss.Style
	followStyle      lipgloss.Style

	// Card chrome
	cardStyle      lipgloss.Style
	cardSelStyle   lipgloss.Style
	tagStyle       lipgloss.Style
	cardTitleStyle lipgloss.Style // Fg, +Bold when Active.Minimal

	// Palette
	promptStyle       lipgloss.Style
	inputStyle        lipgloss.Style
	descStyle         lipgloss.Style
	selItemStyle      lipgloss.Style
	itemStyle         lipgloss.Style
	worktreeChipStyle lipgloss.Style

	// Sessions filter bar chips
	filterChipOnStyle  lipgloss.Style
	filterChipOffStyle lipgloss.Style
	filterAllOnStyle   lipgloss.Style
	filterAllOffStyle  lipgloss.Style

	// Workspace switcher chips
	workspaceChipOnStyle  lipgloss.Style
	workspaceChipOffStyle lipgloss.Style

	// Minimal mode
	minimalProjectSelStyle      lipgloss.Style // Primary + Bold
	minimalTagTextStyle         lipgloss.Style // Fg
	minimalTagBranchPrefixStyle lipgloss.Style // Running
	minimalSeparatorStyle       lipgloss.Style // Dim
)

// ApplyTheme switches the active theme by name. Unknown names fall back to
// "default" so callers can pass user-supplied config without pre-validation.
func ApplyTheme(name string) {
	t, ok := Themes[name]
	if !ok {
		t = Themes["default"]
	}
	Active = t
	rebuildHeaderStyles(t)
	rebuildStateStyles(t)
	rebuildLogStyles(t)
	rebuildCardStyles(t)
	rebuildPaletteStyles(t)
	rebuildFilterStyles(t)
	rebuildWorkspaceStyles(t)
	rebuildMinimalStyles(t)
	rebuildSpinnerPalette(t)
}

func rebuildHeaderStyles(t Theme) {
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(t.Primary)
	badgeStyle = lipgloss.NewStyle().Foreground(t.Muted)
	sectionStyle = lipgloss.NewStyle().Foreground(t.Dim)
	projectStyle = lipgloss.NewStyle().Bold(true).Foreground(t.Fg)
	selectedStyle = lipgloss.NewStyle().Background(t.SelBg).Foreground(t.SelFg)
	helpKeyStyle = lipgloss.NewStyle().Foreground(t.Fg).Bold(true)
	mutedStyle = lipgloss.NewStyle().Foreground(t.Muted)
}

func rebuildStateStyles(t Theme) {
	runningStyle = lipgloss.NewStyle().Foreground(t.Running)
	waitingStyle = lipgloss.NewStyle().Foreground(t.Waiting)
	idleStyle = lipgloss.NewStyle().Foreground(t.Idle)
	stoppedStyle = lipgloss.NewStyle().Foreground(t.Stopped)
	pendingStyle = lipgloss.NewStyle().Foreground(t.Pending)
}

func rebuildLogStyles(t Theme) {
	activeTabStyle = lipgloss.NewStyle().Bold(true).Foreground(t.Primary)
	inactiveTabStyle = lipgloss.NewStyle().Foreground(t.Muted)
	logWarnStyle = lipgloss.NewStyle().Foreground(t.Warn)
	logErrorStyle = lipgloss.NewStyle().Foreground(t.Error)
	logDebugStyle = lipgloss.NewStyle().Foreground(t.Muted)
	followStyle = lipgloss.NewStyle().Foreground(t.Running)
}

func rebuildCardStyles(t Theme) {
	cardStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Dim).
		Padding(0, 1)
	cardSelStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary).
		Padding(0, 1)
	tagStyle = lipgloss.NewStyle().
		Foreground(t.TagFg).
		Background(t.Accent).
		Padding(0, 1)
	cardTitleStyle = lipgloss.NewStyle().Foreground(t.Fg)
	if t.Minimal {
		cardTitleStyle = cardTitleStyle.Bold(true)
	}
}

func rebuildPaletteStyles(t Theme) {
	promptStyle = lipgloss.NewStyle().Foreground(t.Primary).Bold(true)
	inputStyle = lipgloss.NewStyle().Foreground(t.Fg)
	descStyle = lipgloss.NewStyle().Foreground(t.Muted)
	selItemStyle = lipgloss.NewStyle().Background(t.SelBg).Foreground(t.SelFg)
	itemStyle = lipgloss.NewStyle()
	worktreeChipStyle = lipgloss.NewStyle().Background(t.Primary).Foreground(t.TagFg).Bold(true)
}

func rebuildFilterStyles(t Theme) {
	filterChipOnStyle = lipgloss.NewStyle().
		Background(t.SelBg).
		Padding(0, 1)
	filterChipOffStyle = lipgloss.NewStyle().
		Foreground(t.Dim).
		Padding(0, 1)
	filterAllOnStyle = lipgloss.NewStyle().
		Bold(true).
		Background(t.SelBg).
		Foreground(t.Primary).
		Padding(0, 1)
	filterAllOffStyle = lipgloss.NewStyle().
		Foreground(t.Dim).
		Padding(0, 1)
}

func rebuildWorkspaceStyles(t Theme) {
	workspaceChipOnStyle = lipgloss.NewStyle().
		Background(t.Primary).
		Foreground(t.TagFg).
		Bold(true).
		Padding(0, 1)
	workspaceChipOffStyle = lipgloss.NewStyle().
		Foreground(t.Muted).
		Padding(0, 1)
}

func rebuildMinimalStyles(t Theme) {
	minimalProjectSelStyle = lipgloss.NewStyle().Foreground(t.Primary).Bold(true)
	minimalTagTextStyle = lipgloss.NewStyle().Foreground(t.Fg)
	minimalTagBranchPrefixStyle = lipgloss.NewStyle().Foreground(t.Running)
	minimalSeparatorStyle = lipgloss.NewStyle().Foreground(t.Dim)
}

func stateStyle(s state.Status) lipgloss.Style {
	switch s {
	case state.StatusRunning:
		return runningStyle
	case state.StatusWaiting:
		return waitingStyle
	case state.StatusIdle:
		return idleStyle
	case state.StatusStopped:
		return stoppedStyle
	case state.StatusPending:
		return pendingStyle
	default:
		return idleStyle
	}
}

// stateColor returns the foreground color associated with a session status.
// Used by the filter bar so chips can wear their status color while keeping
// the shared chip background.
func stateColor(s state.Status) color.Color {
	switch s {
	case state.StatusRunning:
		return Active.Running
	case state.StatusWaiting:
		return Active.Waiting
	case state.StatusIdle:
		return Active.Idle
	case state.StatusStopped:
		return Active.Stopped
	case state.StatusPending:
		return Active.Pending
	default:
		return Active.Fg
	}
}

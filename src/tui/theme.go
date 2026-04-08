package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/take/agent-roost/session"
)

// Theme holds the colors used across every TUI screen.
// Tweak DefaultTheme to re-skin the whole app.
type Theme struct {
	Primary color.Color // titles, accents
	Accent  color.Color // tags, secondary highlights
	Fg      color.Color // default text on cards/panels
	Muted   color.Color // secondary info (last prompt, subjects)
	Dim     color.Color // help, borders, background noise
	SelBg   color.Color // selected row background
	SelFg   color.Color // selected row foreground

	Running color.Color
	Waiting color.Color
	Idle    color.Color
	Stopped color.Color
	Pending color.Color

	Warn  color.Color
	Error color.Color
}

// DefaultTheme is the dark Gruvbox-ish palette used by the TUI.
var DefaultTheme = Theme{
	Primary: lipgloss.Color("#7D56F4"),
	Accent:  lipgloss.Color("#7D56F4"),
	Fg:      lipgloss.Color("#EBDBB2"),
	Muted:   lipgloss.Color("#888888"),
	Dim:     lipgloss.Color("#626262"),
	SelBg:   lipgloss.Color("#3C3836"),
	SelFg:   lipgloss.Color("#EBDBB2"),

	Running: lipgloss.Color("#00ff00"),
	Waiting: lipgloss.Color("#ffff00"),
	Idle:    lipgloss.Color("#888888"),
	Stopped: lipgloss.Color("#ff0000"),
	Pending: lipgloss.Color("#ff8800"),

	Warn:  lipgloss.Color("#ffff00"),
	Error: lipgloss.Color("#ff0000"),
}

// Pre-built styles. These are package-level so we don't rebuild them per frame.
var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(DefaultTheme.Primary)
	badgeStyle    = lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	sectionStyle  = lipgloss.NewStyle().Foreground(DefaultTheme.Dim)
	projectStyle  = lipgloss.NewStyle().Bold(true).Foreground(DefaultTheme.Fg)
	selectedStyle = lipgloss.NewStyle().Background(DefaultTheme.SelBg).Foreground(DefaultTheme.SelFg)
	helpStyle     = lipgloss.NewStyle().Foreground(DefaultTheme.Dim)
	helpKeyStyle  = lipgloss.NewStyle().Foreground(DefaultTheme.Fg).Bold(true)
	tagStyle      = lipgloss.NewStyle().Foreground(DefaultTheme.Accent)
	mutedStyle    = lipgloss.NewStyle().Foreground(DefaultTheme.Muted)

	runningStyle = lipgloss.NewStyle().Foreground(DefaultTheme.Running)
	waitingStyle = lipgloss.NewStyle().Foreground(DefaultTheme.Waiting)
	idleStyle    = lipgloss.NewStyle().Foreground(DefaultTheme.Idle)
	stoppedStyle = lipgloss.NewStyle().Foreground(DefaultTheme.Stopped)
	pendingStyle = lipgloss.NewStyle().Foreground(DefaultTheme.Pending)

	// Log screen
	activeTabStyle   = lipgloss.NewStyle().Bold(true).Foreground(DefaultTheme.Primary)
	inactiveTabStyle = lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	logWarnStyle     = lipgloss.NewStyle().Foreground(DefaultTheme.Warn)
	logErrorStyle    = lipgloss.NewStyle().Foreground(DefaultTheme.Error)
	logDebugStyle    = lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	followStyle      = lipgloss.NewStyle().Foreground(DefaultTheme.Running)

	// Panel / card chrome
	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(DefaultTheme.Dim).
			Padding(0, 1)
	cardSelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(DefaultTheme.Primary).
			Padding(0, 1)

	// Palette
	promptStyle  = lipgloss.NewStyle().Foreground(DefaultTheme.Primary).Bold(true)
	inputStyle   = lipgloss.NewStyle().Foreground(DefaultTheme.Fg)
	descStyle    = lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	selItemStyle = lipgloss.NewStyle().Background(DefaultTheme.SelBg).Foreground(DefaultTheme.SelFg)
	itemStyle    = lipgloss.NewStyle()
)

func stateStyle(s session.State) lipgloss.Style {
	switch s {
	case session.StateRunning:
		return runningStyle
	case session.StateWaiting:
		return waitingStyle
	case session.StateIdle:
		return idleStyle
	case session.StateStopped:
		return stoppedStyle
	case session.StatePending:
		return pendingStyle
	default:
		return idleStyle
	}
}

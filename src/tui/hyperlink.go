package tui

import (
	"os"

	"charm.land/lipgloss/v2"
)

var hyperlinksActive = os.Getenv("ROOST_HYPERLINKS") != "off"

// Link wraps text in an OSC 8 hyperlink using the terminal's native support
// via lipgloss. Returns plain text when hyperlinks are disabled or url is empty.
func Link(url, text string) string {
	if !hyperlinksActive || url == "" {
		return text
	}
	return lipgloss.NewStyle().Hyperlink(url).Render(text)
}

// SetHyperlinksActive enables or disables OSC 8 output globally.
func SetHyperlinksActive(on bool) { hyperlinksActive = on }

// HyperlinksActive reports the current state.
func HyperlinksActive() bool { return hyperlinksActive }

// fileLink returns a file:// URL for the given absolute path.
func fileLink(absPath string) string {
	if absPath == "" {
		return ""
	}
	return "file://localhost" + absPath
}

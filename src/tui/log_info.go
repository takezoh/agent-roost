package tui

import (
	"fmt"
	"strings"

	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/tui/glyphs"
)

// renderInfoContent builds the INFO tab body. The TUI prepends a generic
// header from SessionInfo (ID / Project / Command / State /
// Created / StateChanged) and then appends the driver-specific
// View.InfoExtras lines, followed by the driver's Indicators / Tags
// chips for at-a-glance debugging. Driver-side rendering is
// intentionally minimal: the TUI controls layout and ordering of the
// generic block so every session shows the same header in the same order.
//
// The second return value is the 0-indexed line number of the Project
// row within the body (or -1 if there is no Project row). Click
// hit-testing uses it to map a y coordinate to the link target.
func renderInfoContent(s *proto.SessionInfo, width int) (string, int) {
	if s == nil {
		return "", -1
	}
	var b strings.Builder
	lineCount := 0
	projectLine := -1
	writeField := func(label, value string) {
		if value == "" {
			return
		}
		fmt.Fprintf(&b, "%-13s %s\n", label+":", value)
		lineCount++
	}

	writeField("ID", s.ID)
	const labelCols = 14 // "%-13s " prefix width
	displayProject := truncate(s.Project, width-labelCols)
	if displayProject != "" {
		projectLine = lineCount
	}
	writeField("Project", Link(fileLink(s.Project), displayProject))
	writeField("Command", s.DisplayCommand())
	writeField("State", glyphs.Get(s.State.SymbolKey())+" "+s.State.String())
	if t := s.CreatedAtTime(); !t.IsZero() {
		writeField("Created", t.Format("2006-01-02 15:04:05"))
	}
	if t := s.StateChangedAtTime(); !t.IsZero() {
		writeField("StateChanged", t.Format("2006-01-02 15:04:05"))
	}

	for _, line := range s.View.InfoExtras {
		writeField(line.Label, line.Value)
	}

	if len(s.View.Card.Indicators) > 0 {
		b.WriteString("\nIndicators:\n")
		for _, ind := range s.View.Card.Indicators {
			fmt.Fprintf(&b, "  • %s\n", ind)
		}
	}
	if len(s.View.Card.Tags) > 0 {
		b.WriteString("\nTags:\n")
		for _, tag := range s.View.Card.Tags {
			fmt.Fprintf(&b, "  • %s\n", tag.Text)
		}
	}

	return strings.TrimRight(b.String(), "\n"), projectLine
}

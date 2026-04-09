package tui

import (
	"fmt"
	"strings"

	"github.com/take/agent-roost/core"
)

func formatSessionInfo(s *core.SessionInfo) string {
	if s == nil {
		return ""
	}

	var b strings.Builder
	writeField := func(label, value string) {
		if value == "" {
			return
		}
		fmt.Fprintf(&b, "%-13s %s\n", label+":", value)
	}

	writeField("ID", s.ID)
	writeField("Project", s.Project)
	writeField("WindowID", s.WindowID)
	writeField("Command", s.DisplayCommand())
	writeField("State", s.State.Symbol()+" "+s.State.String())
	if t := s.CreatedAtTime(); !t.IsZero() {
		writeField("Created", t.Format("2006-01-02 15:04:05"))
	}
	if t := s.StateChangedAtTime(); !t.IsZero() {
		writeField("StateChanged", t.Format("2006-01-02 15:04:05"))
	}
	writeField("Title", s.Title)
	writeField("LastPrompt", s.LastPrompt)

	if len(s.Subjects) > 0 {
		b.WriteString("\nSubjects:\n")
		for _, subj := range s.Subjects {
			fmt.Fprintf(&b, "  • %s\n", subj)
		}
	}
	if len(s.Indicators) > 0 {
		b.WriteString("\nIndicators:\n")
		for _, ind := range s.Indicators {
			fmt.Fprintf(&b, "  • %s\n", ind)
		}
	}
	if len(s.Tags) > 0 {
		b.WriteString("\nTags:\n")
		for _, tag := range s.Tags {
			fmt.Fprintf(&b, "  • %s\n", tag.Text)
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

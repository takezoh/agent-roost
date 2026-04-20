package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/takezoh/agent-roost/proto"
)

// frameTabLayout renders a row of frame tab chips for the active session
// and returns the hitboxes for mouse click detection. Pure function.
func frameTabLayout(sess proto.SessionInfo) (string, []frameTabHitbox) {
	if len(sess.Frames) == 0 {
		return "", nil
	}
	var parts []string
	boxes := make([]frameTabHitbox, 0, len(sess.Frames))
	x := 0

	for i, f := range sess.Frames {
		label := frameTabLabel(f.Command, i)
		var rendered string
		if f.ID == sess.ActiveFrameID {
			rendered = activeTabStyle.Render("[" + label + "]")
		} else {
			rendered = inactiveTabStyle.Render(label)
		}
		w := lipgloss.Width(rendered)
		boxes = append(boxes, frameTabHitbox{
			sessionID: sess.ID,
			frameID:   f.ID,
			x0:        x,
			x1:        x + w,
		})
		parts = append(parts, rendered)
		x += w + 1
	}
	return strings.Join(parts, " "), boxes
}

// frameTabLabel returns a short display label for a frame tab.
func frameTabLabel(command string, index int) string {
	if command == "" {
		if index == 0 {
			return "root"
		}
		return "idle"
	}
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return command
	}
	return parts[0]
}

func (m HeaderModel) View() tea.View {
	line := m.renderTabLine()
	v := tea.NewView(line)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	return v
}

func (m HeaderModel) renderTabLine() string {
	var active *proto.SessionInfo
	for i := range m.sessions {
		if m.sessions[i].IsActive {
			active = &m.sessions[i]
			break
		}
	}
	if active == nil {
		return mutedStyle.Render("roost")
	}
	line, _ := frameTabLayout(*active)
	return line
}

package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/take/agent-roost/state"
)

// Panel renders body inside a rounded border, with title on the top-left
// and an optional badge on the top-right of the border line.
//
// outerWidth is the total rendered width (including borders + padding).
// In lipgloss v2, Style.Width() sets the total outer width, so we pass it
// through directly.
func Panel(title, badge, body string, outerWidth int) string {
	const minOuter = 20
	if outerWidth < minOuter {
		outerWidth = minOuter
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(Active.Dim).
		Padding(0, 1).
		Width(outerWidth)

	rendered := style.Render(body)
	return overlayBorderTitle(rendered, title, badge, outerWidth)
}

// Card wraps body in a small rounded border. When selected, the border color
// is switched to the accent color instead of a dim line.
// outerWidth is the total width including borders + padding.
func Card(body string, selected bool, outerWidth int, borderTitle state.Tag, borderBadge string) string {
	if outerWidth < 8 {
		outerWidth = 8
	}
	style := cardStyle
	if selected {
		style = cardSelStyle
	}
	rendered := style.Width(outerWidth).Render(body)
	if borderTitle.Text != "" || borderBadge != "" {
		fg := Active.Dim
		if selected {
			fg = Active.Primary
		}
		rendered = overlayCardBorderTitle(rendered, borderTitle, borderBadge, outerWidth, fg)
	}
	return rendered
}

// PanelChromeRows is the number of rows the Panel adds around the body
// (top border + bottom border). Padding is horizontal-only so rows unchanged.
const PanelChromeRows = 2

// overlayBorderTitle replaces the first line of `rendered` with a new top
// border line that has title on the left and badge on the right.
//
// outerWidth is the total rendered width (matches Style.Width in lipgloss v2).
// The top border rendered by lipgloss has length outerWidth:
//
//	╭ + ─×(outerWidth-2) + ╮
func overlayBorderTitle(rendered, title, badge string, outerWidth int) string {
	lines := strings.Split(rendered, "\n")
	if len(lines) == 0 {
		return rendered
	}

	middleW := outerWidth - 2 // cells between the corners
	if middleW < 4 {
		return rendered
	}

	// Reserved cells for title/badge chunks: dash + space + text + space.
	titleW := 1
	if title != "" {
		titleW = 3 + lipgloss.Width(title)
	}
	badgeW := 1
	if badge != "" {
		badgeW = 3 + lipgloss.Width(badge)
	}

	fill := middleW - titleW - badgeW
	if fill < 0 {
		// Fall back to the original border line when we can't fit.
		return rendered
	}

	var b strings.Builder
	b.WriteString(sectionStyle.Render("╭"))
	if title != "" {
		b.WriteString(sectionStyle.Render("─ "))
		b.WriteString(titleStyle.Render(title))
		b.WriteString(sectionStyle.Render(" "))
	} else {
		b.WriteString(sectionStyle.Render("─"))
	}
	b.WriteString(sectionStyle.Render(strings.Repeat("─", fill)))
	if badge != "" {
		b.WriteString(sectionStyle.Render(" "))
		b.WriteString(badgeStyle.Render(badge))
		b.WriteString(sectionStyle.Render(" ─"))
	} else {
		b.WriteString(sectionStyle.Render("─"))
	}
	b.WriteString(sectionStyle.Render("╮"))

	lines[0] = b.String()
	return strings.Join(lines, "\n")
}

// overlayCardBorderTitle is like overlayBorderTitle but uses the given
// border foreground color instead of the shared sectionStyle/titleStyle.
// This lets Card() match the overlay color to the card's border
// (Dim for normal, Primary for selected).
func overlayCardBorderTitle(rendered string, title state.Tag, badge string, outerWidth int, fg color.Color) string {
	lines := strings.Split(rendered, "\n")
	if len(lines) == 0 {
		return rendered
	}

	middleW := outerWidth - 2
	if middleW < 4 {
		return rendered
	}

	titleW := 1
	if title.Text != "" {
		titleW = 3 + lipgloss.Width(title.Text)
	}
	badgeW := 1
	if badge != "" {
		badgeW = 3 + lipgloss.Width(badge)
	}

	fill := middleW - titleW - badgeW
	if fill < 0 && badge != "" {
		// Badge too long — try truncating it.
		// Minimum badge frame: " X… ─" = 4 chars around the badge text.
		maxBadge := middleW - titleW - 4
		if maxBadge >= 4 {
			badge = truncate(badge, maxBadge)
			badgeW = 3 + lipgloss.Width(badge)
			fill = middleW - titleW - badgeW
		} else {
			// Drop badge entirely, show title only.
			badge = ""
			badgeW = 1
			fill = middleW - titleW - badgeW
		}
	}
	if fill < 0 {
		return rendered
	}

	border := lipgloss.NewStyle().Foreground(fg)

	var b strings.Builder
	b.WriteString(border.Render("╭"))
	if title.Text != "" {
		b.WriteString(border.Render("─"))
		b.WriteString(renderTag(title))
	} else {
		b.WriteString(border.Render("─"))
	}
	b.WriteString(border.Render(strings.Repeat("─", fill)))
	if badge != "" {
		b.WriteString(border.Render(" "))
		b.WriteString(mutedStyle.Render(badge))
		b.WriteString(border.Render(" ─"))
	} else {
		b.WriteString(border.Render("─"))
	}
	b.WriteString(border.Render("╮"))

	lines[0] = b.String()
	return strings.Join(lines, "\n")
}

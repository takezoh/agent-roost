package tui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"github.com/take/agent-roost/core"
)

const (
	tailPollInterval = 200 * time.Millisecond
	tailInitialBytes = 128 * 1024
	maxLogLines      = 5000
)

type tickMsg time.Time
type logTab int


type logEventMsg core.Message
type logDisconnectMsg struct{}

type tabState struct {
	label   string
	logPath string
	file    *os.File
	offset  int64
	buf     string
}

type LogModel struct {
	viewport   viewport.Model
	activeTab  logTab
	appLogPath string
	tabs       []*tabState

	following bool
	width     int
	height    int
	client    *core.Client
}

func NewLogModel(appLogPath string, client *core.Client) LogModel {
	return LogModel{
		appLogPath: appLogPath,
		tabs: []*tabState{
			{label: "LOG", logPath: appLogPath},
		},
		client:    client,
		activeTab: 0,
		following: true,
	}
}

func (m LogModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		func() tea.Msg { return tickMsg(time.Now()) },
	}
	if m.client != nil {
		cmds = append(cmds, m.listenEvents())
	}
	return tea.Batch(cmds...)
}

func (m LogModel) listenEvents() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-m.client.Events()
		if !ok {
			return logDisconnectMsg{}
		}
		return logEventMsg(msg)
	}
}

func (m LogModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		headerHeight := 2
		m.viewport.SetWidth(m.width)
		m.viewport.SetHeight(m.height - headerHeight)
		return m, nil

	case tickMsg:
		tab := m.activeTabState()
		if tab != nil {
			newContent, err := readNewLines(tab)
			if err == nil && newContent != "" {
				m.appendContent(newContent)
			}
		}
		if m.following {
			m.viewport.GotoBottom()
		}
		return m, tickCmd()

	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, nil
		case "G":
			m.following = true
			m.viewport.GotoBottom()
			return m, nil
		case "g":
			m.following = false
			m.viewport.GotoTop()
			return m, nil
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		m.following = m.viewport.AtBottom()
		return m, cmd

	case logEventMsg:
		coreMsg := core.Message(msg)
		switch coreMsg.Event {
		case "sessions-changed":
			m.rebuildTabs(coreMsg.EventLogPath, coreMsg.TranscriptPath)
		}
		if m.client != nil {
			return m, m.listenEvents()
		}
		return m, nil

	case logDisconnectMsg:
		m.client = nil
		return m, nil

	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		if mouse.Y == 0 && mouse.Button == tea.MouseLeft {
			m.switchToTab(m.tabIndexAtX(mouse.X))
		}
		return m, nil
	}
	return m, nil
}

func (m *LogModel) rebuildTabs(eventLogPath, transcriptPath string) {
	// Detect if active session changed (transcript path differs)
	prevTranscript := ""
	for _, t := range m.tabs {
		if t.label == "TRANSCRIPT" {
			prevTranscript = t.logPath
			break
		}
	}
	sessionChanged := transcriptPath != prevTranscript

	// Close old dynamic tab files
	for _, t := range m.tabs {
		if t.label != "LOG" && t.file != nil {
			t.file.Close()
		}
	}

	// Rebuild: TRANSCRIPT + EVENTS (Claude) + LOG (always last)
	var tabs []*tabState
	if transcriptPath != "" {
		tabs = append(tabs, &tabState{label: "TRANSCRIPT", logPath: transcriptPath})
	}
	if eventLogPath != "" {
		tabs = append(tabs, &tabState{label: "EVENTS", logPath: eventLogPath})
	}
	tabs = append(tabs, &tabState{label: "LOG", logPath: m.appLogPath})
	m.tabs = tabs

	if int(m.activeTab) >= len(m.tabs) {
		m.activeTab = 0
	}

	// Default to TRANSCRIPT only when active session changes
	if sessionChanged && transcriptPath != "" {
		m.switchToTab(0)
	}
}

func (m *LogModel) isLogTab() bool {
	tab := m.activeTabState()
	return tab != nil && tab.label == "LOG"
}

func (m *LogModel) activeTabState() *tabState {
	idx := int(m.activeTab)
	if idx >= 0 && idx < len(m.tabs) {
		return m.tabs[idx]
	}
	return nil
}

func (m *LogModel) switchToTab(tab logTab) {
	if tab == m.activeTab {
		return
	}
	m.activeTab = tab

	// Reset reader to tail from end of file
	if t := m.activeTabState(); t != nil {
		if t.file != nil {
			t.file.Close()
			t.file = nil
		}
		t.offset = 0
		t.buf = ""
		m.viewport.SetContent("")
		m.following = true
	}
}

func (m *LogModel) tabIndexAtX(x int) logTab {
	pos := 0
	for i, tab := range m.tabs {
		w := len([]rune(tab.label)) + 1
		if x < pos+w {
			return logTab(i)
		}
		pos += w
	}
	return m.activeTab
}

func (m *LogModel) appendContent(newContent string) {
	var styled string
	if m.isLogTab() {
		styled = colorizeLines(newContent)
	} else {
		styled = newContent
	}
	existing := m.viewport.GetContent()
	var content string
	if existing == "" {
		content = styled
	} else {
		content = existing + "\n" + styled
	}
	content = trimLines(content, maxLogLines)
	m.viewport.SetContent(content)
}

func readNewLines(tab *tabState) (string, error) {
	if tab.file == nil {
		f, err := os.Open(tab.logPath)
		if err != nil {
			return "", err
		}
		tab.file = f
		info, err := f.Stat()
		if err != nil {
			return "", err
		}
		size := info.Size()
		if size > tailInitialBytes {
			tab.offset = size - tailInitialBytes
		}
	}

	info, err := tab.file.Stat()
	if err != nil {
		tab.file.Close()
		tab.file = nil
		return "", err
	}
	if info.Size() < tab.offset {
		tab.file.Close()
		tab.file = nil
		tab.offset = 0
		tab.buf = ""
		return "", nil
	}
	if info.Size() == tab.offset {
		return "", nil
	}

	tab.file.Seek(tab.offset, io.SeekStart)
	data, err := io.ReadAll(io.LimitReader(tab.file, info.Size()-tab.offset))
	if err != nil {
		return "", err
	}
	tab.offset += int64(len(data))

	text := tab.buf + string(data)
	if !strings.HasSuffix(text, "\n") {
		lastNL := strings.LastIndex(text, "\n")
		if lastNL < 0 {
			tab.buf = text
			return "", nil
		}
		tab.buf = text[lastNL+1:]
		text = text[:lastNL]
	} else {
		tab.buf = ""
		text = strings.TrimRight(text, "\n")
	}
	return text, nil
}

func trimLines(content string, max int) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= max {
		return content
	}
	return strings.Join(lines[len(lines)-max:], "\n")
}

func tickCmd() tea.Cmd {
	return tea.Tick(tailPollInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// --- view ---

var (
	activeTabStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	inactiveTabStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	logWarnStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffff00"))
	logErrorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000"))
	logDebugStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	followStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff00"))
)

func (m LogModel) View() tea.View {
	var b strings.Builder
	b.WriteString(m.renderTabHeader())

	if m.following {
		b.WriteString(" " + followStyle.Render("↓"))
	} else {
		b.WriteString(" " + logDebugStyle.Render(fmt.Sprintf("%.0f%%", m.viewport.ScrollPercent()*100)))
	}
	b.WriteString("\n")

	if len(m.tabs) == 0 {
		b.WriteString(inactiveTabStyle.Render("  No sessions"))
	} else {
		b.WriteString(m.viewport.View())
	}

	v := tea.NewView(b.String())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m LogModel) renderTabHeader() string {
	var b strings.Builder
	for i, tab := range m.tabs {
		if i > 0 {
			b.WriteString(" ")
		}
		if logTab(i) == m.activeTab {
			b.WriteString(activeTabStyle.Render("[" + tab.label + "]"))
		} else {
			b.WriteString(inactiveTabStyle.Render(tab.label))
		}
	}
	return b.String()
}

func colorizeLines(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = colorizeLogLine(line)
	}
	return strings.Join(lines, "\n")
}

func colorizeLogLine(line string) string {
	level := parseLogLevel(line)
	switch level {
	case "ERROR":
		return logErrorStyle.Render(line)
	case "WARN":
		return logWarnStyle.Render(line)
	case "DEBUG":
		return logDebugStyle.Render(line)
	default:
		return line
	}
}

func parseLogLevel(line string) string {
	idx := strings.Index(line, "level=")
	if idx < 0 {
		return ""
	}
	rest := line[idx+6:]
	end := strings.IndexByte(rest, ' ')
	if end < 0 {
		return rest
	}
	return rest[:end]
}

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
	tailInitialBytes = 4096
	maxLogLines      = 5000
)

type tickMsg time.Time

type logTab int

const (
	tabApp logTab = iota
	tabSession
)

type logEventMsg core.Message
type logDisconnectMsg struct{}

type LogModel struct {
	viewport       viewport.Model
	activeTab      logTab
	appLogPath     string
	sessionLogPath string

	logPath  string
	file     *os.File
	offset   int64
	buf      string

	following bool
	width     int
	height    int
	client    *core.Client
}

func NewLogModel(appLogPath string, client *core.Client) LogModel {
	return LogModel{
		appLogPath: appLogPath,
		logPath:    appLogPath,
		client:     client,
		activeTab:  tabApp,
		following:  true,
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
		newContent, err := m.readNewLines()
		if err == nil && newContent != "" {
			m.appendContent(newContent)
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
		if coreMsg.SessionLogPath != "" {
			m.sessionLogPath = coreMsg.SessionLogPath
			if m.activeTab == tabSession {
				m.switchToFile(m.sessionLogPath)
			}
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
			appWidth := 5
			if mouse.X < appWidth {
				m.switchToTab(tabApp)
			} else {
				m.switchToTab(tabSession)
			}
		}
		return m, nil
	}
	return m, nil
}

func (m *LogModel) switchToFile(path string) {
	m.closeFile()
	m.resetReader(path)
}

func (m *LogModel) closeFile() {
	if m.file != nil {
		m.file.Close()
		m.file = nil
	}
}

func (m *LogModel) resetReader(path string) {
	m.logPath = path
	m.offset = 0
	m.buf = ""
	m.viewport.SetContent("")
	m.following = true
}

func (m *LogModel) switchToTab(tab logTab) {
	if tab == m.activeTab {
		return
	}
	m.activeTab = tab
	switch tab {
	case tabApp:
		m.switchToFile(m.appLogPath)
	case tabSession:
		if m.sessionLogPath != "" {
			m.switchToFile(m.sessionLogPath)
		}
	}
}

func (m *LogModel) appendContent(newContent string) {
	var styled string
	if m.activeTab == tabApp {
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

func (m *LogModel) readNewLines() (string, error) {
	if m.file == nil {
		f, err := os.Open(m.logPath)
		if err != nil {
			return "", err
		}
		m.file = f
		info, err := f.Stat()
		if err != nil {
			return "", err
		}
		size := info.Size()
		if size > tailInitialBytes {
			m.offset = size - tailInitialBytes
		}
	}

	info, err := m.file.Stat()
	if err != nil {
		m.file.Close()
		m.file = nil
		return "", err
	}
	if info.Size() < m.offset {
		m.file.Close()
		m.file = nil
		m.offset = 0
		m.buf = ""
		return "", nil
	}
	if info.Size() == m.offset {
		return "", nil
	}

	m.file.Seek(m.offset, io.SeekStart)
	data, err := io.ReadAll(io.LimitReader(m.file, info.Size()-m.offset))
	if err != nil {
		return "", err
	}
	m.offset += int64(len(data))

	text := m.buf + string(data)
	if !strings.HasSuffix(text, "\n") {
		lastNL := strings.LastIndex(text, "\n")
		if lastNL < 0 {
			m.buf = text
			return "", nil
		}
		m.buf = text[lastNL+1:]
		text = text[:lastNL]
	} else {
		m.buf = ""
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

	var appLabel, sessionLabel string
	if m.activeTab == tabApp {
		appLabel = activeTabStyle.Render("[APP]")
		sessionLabel = inactiveTabStyle.Render(" SESSION")
	} else {
		appLabel = inactiveTabStyle.Render(" APP ")
		sessionLabel = activeTabStyle.Render("[SESSION]")
	}
	header := appLabel + sessionLabel

	if m.following {
		header += " " + followStyle.Render("↓")
	} else {
		header += " " + logDebugStyle.Render(fmt.Sprintf("%.0f%%", m.viewport.ScrollPercent()*100))
	}
	b.WriteString(header)
	b.WriteString("\n")

	if m.activeTab == tabSession && m.sessionLogPath == "" {
		b.WriteString(inactiveTabStyle.Render("  セッションなし"))
	} else {
		b.WriteString(m.viewport.View())
	}

	v := tea.NewView(b.String())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
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

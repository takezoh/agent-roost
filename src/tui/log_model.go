package tui

import (
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/viewport"
	"github.com/take/agent-roost/core"
	"github.com/take/agent-roost/lib/claude/transcript"
)

const (
	tailPollInterval = 200 * time.Millisecond
	// initialBackfillLines bounds how much past content is shown when a
	// session becomes active (or a tab is first opened). The unit matches
	// maxLogLines below so the cap and the backfill speak the same units.
	initialBackfillLines = 2000
	// tailReadChunk is the chunk size used when scanning backwards from
	// EOF looking for the Nth-from-last newline.
	tailReadChunk = 64 * 1024
	maxLogLines   = 5000
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

	following      bool
	width          int
	height         int
	client         *core.Client
	parser         *transcript.Parser
	showThinking   bool
	currentSession *core.SessionInfo
}

func NewLogModel(appLogPath string, client *core.Client, showThinking bool) LogModel {
	return LogModel{
		appLogPath: appLogPath,
		tabs: []*tabState{
			{label: "LOG", logPath: appLogPath},
		},
		client:       client,
		activeTab:    0,
		following:    true,
		showThinking: showThinking,
		parser:       transcript.NewParser(transcript.ParserOptions{ShowThinking: showThinking}),
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
		return m.handleResize(msg)
	case tickMsg:
		return m.handleTick()
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case logEventMsg:
		return m.handleLogEvent(core.Message(msg))
	case logDisconnectMsg:
		m.client = nil
		return m, nil
	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)
	}
	return m, nil
}

func (m LogModel) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	// Reserve 1 row for the tab header line.
	m.viewport.SetWidth(m.width)
	m.viewport.SetHeight(m.height - 1)
	return m, nil
}

func (m LogModel) handleTick() (tea.Model, tea.Cmd) {
	tab := m.activeTabState()
	if tab != nil && tab.logPath != "" {
		newContent, err := readNewLines(tab)
		if err == nil && newContent != "" {
			m.appendContent(newContent)
		}
	}
	if m.following {
		m.viewport.GotoBottom()
	}
	return m, tickCmd()
}

func (m LogModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
	case "t":
		if m.isTranscriptTab() {
			m.toggleThinking()
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	m.following = m.viewport.AtBottom()
	return m, cmd
}

func (m LogModel) handleLogEvent(msg core.Message) (tea.Model, tea.Cmd) {
	if msg.Event == "sessions-changed" {
		m.currentSession = pickActiveSession(msg.Sessions, msg.ActiveWindowID)
		m.rebuildTabs(msg.EventLogPath, msg.TranscriptPath, m.currentSession)
		if msg.IsPreview {
			if idx, ok := m.tabIndexByLabel("INFO"); ok {
				m.activeTab = idx
				m.renderInfoTab()
				m.following = true
			}
		} else if m.activeTabIs("INFO") {
			m.renderInfoTab()
		}
	}
	if m.client != nil {
		return m, m.listenEvents()
	}
	return m, nil
}

func pickActiveSession(sessions []core.SessionInfo, activeWID string) *core.SessionInfo {
	if activeWID == "" {
		return nil
	}
	for i := range sessions {
		if sessions[i].WindowID == activeWID {
			return &sessions[i]
		}
	}
	return nil
}

func (m LogModel) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	mouse := msg.Mouse()
	if mouse.Y == 0 && mouse.Button == tea.MouseLeft {
		m.switchToTab(m.tabIndexAtX(mouse.X))
	}
	return m, nil
}

func (m *LogModel) rebuildTabs(eventLogPath, transcriptPath string, current *core.SessionInfo) {
	prev := make(map[string]*tabState, len(m.tabs))
	for _, t := range m.tabs {
		prev[t.label] = t
	}
	prevTranscript := ""
	if t, ok := prev["TRANSCRIPT"]; ok {
		prevTranscript = t.logPath
	}
	sessionChanged := transcriptPath != prevTranscript

	m.tabs = buildTabList(prev, eventLogPath, transcriptPath, current, m.appLogPath)
	for _, t := range prev {
		if t.file != nil {
			t.file.Close()
		}
	}
	if int(m.activeTab) >= len(m.tabs) {
		m.activeTab = 0
	}
	if sessionChanged && transcriptPath != "" {
		m.activeTab = 0
		m.viewport.SetContent("")
		m.following = true
		m.rebuildParser(transcriptPath)
	}
}

// buildTabList assembles the ordered tab list, reusing entries from prev
// (and removing each reused entry from the map) when label and path match.
// Order: TRANSCRIPT, EVENTS, INFO, LOG. INFO sits immediately before LOG.
func buildTabList(prev map[string]*tabState, eventLogPath, transcriptPath string, current *core.SessionInfo, appLogPath string) []*tabState {
	reuseOrNew := func(label, path string) *tabState {
		if t, ok := prev[label]; ok && t.logPath == path {
			delete(prev, label)
			return t
		}
		return &tabState{label: label, logPath: path}
	}
	var tabs []*tabState
	if transcriptPath != "" {
		tabs = append(tabs, reuseOrNew("TRANSCRIPT", transcriptPath))
	}
	if eventLogPath != "" {
		tabs = append(tabs, reuseOrNew("EVENTS", eventLogPath))
	}
	if current != nil {
		tabs = append(tabs, reuseOrNew("INFO", ""))
	}
	return append(tabs, reuseOrNew("LOG", appLogPath))
}

// rebuildParser constructs a new transcript Parser pointed at the
// subagent directory that lives next to transcriptPath
// (i.e. {sessionID}/subagents/). Called whenever the active session
// changes or thinking visibility is toggled.
func (m *LogModel) rebuildParser(transcriptPath string) {
	opts := transcript.ParserOptions{ShowThinking: m.showThinking}
	if dir := subagentDir(transcriptPath); dir != "" {
		if _, err := os.Stat(dir); err == nil {
			opts.SubagentFS = os.DirFS(dir)
			opts.SubagentDir = "."
		}
	}
	m.parser = transcript.NewParser(opts)
}

// subagentDir returns the directory that contains the per-session
// subagent files for a given main transcript jsonl path. The expected
// layout is "{sess}.jsonl" -> "{sess}/subagents".
func subagentDir(transcriptPath string) string {
	if transcriptPath == "" {
		return ""
	}
	if !strings.HasSuffix(transcriptPath, ".jsonl") {
		return ""
	}
	base := strings.TrimSuffix(transcriptPath, ".jsonl")
	return base + string(os.PathSeparator) + "subagents"
}

func (m *LogModel) isLogTab() bool {
	return m.activeTabIs("LOG")
}

func (m *LogModel) isTranscriptTab() bool {
	return m.activeTabIs("TRANSCRIPT")
}

func (m *LogModel) activeTabIs(label string) bool {
	tab := m.activeTabState()
	return tab != nil && tab.label == label
}

func (m *LogModel) tabIndexByLabel(label string) (logTab, bool) {
	for i, tab := range m.tabs {
		if tab.label == label {
			return logTab(i), true
		}
	}
	return 0, false
}

func (m *LogModel) renderInfoTab() {
	m.viewport.SetContent(formatSessionInfo(m.currentSession))
}

func (m *LogModel) activeTabState() *tabState {
	idx := int(m.activeTab)
	if idx >= 0 && idx < len(m.tabs) {
		return m.tabs[idx]
	}
	return nil
}

// toggleThinking flips the show-thinking flag and resets the active
// transcript tab so the tail is reparsed under the new setting.
func (m *LogModel) toggleThinking() {
	m.showThinking = !m.showThinking
	t := m.activeTabState()
	transcriptPath := ""
	if t != nil {
		transcriptPath = t.logPath
	}
	m.rebuildParser(transcriptPath)
	if t == nil {
		return
	}
	if t.file != nil {
		t.file.Close()
		t.file = nil
	}
	t.offset = 0
	t.buf = ""
	m.viewport.SetContent("")
	m.following = true
}

func (m *LogModel) switchToTab(tab logTab) {
	if tab == m.activeTab {
		return
	}
	m.activeTab = tab

	t := m.activeTabState()
	if t == nil {
		return
	}
	if t.label == "INFO" {
		m.renderInfoTab()
		m.following = true
		return
	}

	// Reset reader to tail from end of file
	if t.file != nil {
		t.file.Close()
		t.file = nil
	}
	t.offset = 0
	t.buf = ""
	m.viewport.SetContent("")
	m.following = true
	m.parser.Reset()
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
	} else if m.isTranscriptTab() {
		entries := m.parser.ParseLines([]byte(newContent))
		styled = transcript.RenderEntries(entries)
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
	if err := openTabFile(tab); err != nil {
		return "", err
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
	return splitTrailingPartial(tab, tab.buf+string(data)), nil
}

func openTabFile(tab *tabState) error {
	if tab.file != nil {
		return nil
	}
	f, err := os.Open(tab.logPath)
	if err != nil {
		return err
	}
	tab.file = f
	off, err := seekToLastNLines(f, initialBackfillLines)
	if err != nil {
		return err
	}
	tab.offset = off
	return nil
}

// seekToLastNLines returns the byte offset that starts the last n lines of
// f (counting both terminated lines and a final unterminated line). If the
// file has fewer than n lines, or n <= 0, it returns 0. The file's seek
// position is left unchanged.
func seekToLastNLines(f *os.File, n int) (int64, error) {
	if n <= 0 {
		return 0, nil
	}
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := info.Size()
	if size == 0 {
		return 0, nil
	}

	// Detect a trailing newline: when present, the final \n doesn't open
	// a new line, so the (n+1)-th newline from the end marks the start of
	// the desired suffix. When absent, the trailing partial line counts
	// as line #1 from the end, so we only need n newlines back.
	lastByte := make([]byte, 1)
	if _, err := f.ReadAt(lastByte, size-1); err != nil {
		return 0, err
	}
	target := n
	if lastByte[0] == '\n' {
		target = n + 1
	}

	buf := make([]byte, tailReadChunk)
	pos := size
	newlines := 0
	for pos > 0 {
		readSize := int64(len(buf))
		if pos < readSize {
			readSize = pos
		}
		pos -= readSize
		if _, err := f.ReadAt(buf[:readSize], pos); err != nil {
			return 0, err
		}
		for i := readSize - 1; i >= 0; i-- {
			if buf[i] != '\n' {
				continue
			}
			newlines++
			if newlines >= target {
				return pos + i + 1, nil
			}
		}
	}
	return 0, nil
}

func splitTrailingPartial(tab *tabState, text string) string {
	if strings.HasSuffix(text, "\n") {
		tab.buf = ""
		return strings.TrimRight(text, "\n")
	}
	lastNL := strings.LastIndex(text, "\n")
	if lastNL < 0 {
		tab.buf = text
		return ""
	}
	tab.buf = text[lastNL+1:]
	return text[:lastNL]
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


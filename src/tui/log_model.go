package tui

import (
	"io"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/viewport"
	"github.com/take/agent-roost/lib/claude/transcript"
	"github.com/take/agent-roost/proto"
	"github.com/take/agent-roost/state"
)

const (
	// initialBackfillLines bounds how much past content is shown when a
	// session becomes active (or a tab is first opened).
	initialBackfillLines = 2000
	// tailReadChunk is the chunk size used when scanning backwards from
	// EOF looking for the Nth-from-last newline.
	tailReadChunk = 64 * 1024
	maxLogLines   = 5000
)

type logTab int


type logEventMsg struct{ event proto.ServerEvent }
type logDisconnectMsg struct{}

// tabKindLog and tabKindInfo are LogModel-internal kinds used by tabs the
// TUI manages itself (the always-on LOG tab and the synthesized INFO
// tab). Driver-provided tabs carry one of state.TabKind* values.
const (
	tabKindLog  state.TabKind = "_log"
	tabKindInfo state.TabKind = "_info"
)

type tabState struct {
	label   string
	logPath string
	kind    state.TabKind
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
	client         *proto.Client
	parser         *transcript.Parser
	showThinking   bool
	currentSession *proto.SessionInfo
}

func NewLogModel(appLogPath string, client *proto.Client, showThinking bool) LogModel {
	return LogModel{
		appLogPath: appLogPath,
		tabs: []*tabState{
			{label: "LOG", logPath: appLogPath, kind: tabKindLog},
		},
		client:       client,
		activeTab:    0,
		following:    true,
		showThinking: showThinking,
		parser:       transcript.NewParser(transcript.ParserOptions{ShowThinking: showThinking}),
	}
}

func (m LogModel) Init() tea.Cmd {
	var cmds []tea.Cmd
	if m.client != nil {
		cmds = append(cmds, m.listenEvents())
	}
	// Initial backfill: read the tail of the app log file so the LOG
	// tab is not blank at startup. Subsequent content arrives via
	// push events from the daemon's FileRelay.
	cmds = append(cmds, m.backfillActiveTab())
	return tea.Batch(cmds...)
}

func (m LogModel) listenEvents() tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-m.client.Events()
		if !ok {
			return logDisconnectMsg{}
		}
		return logEventMsg{event: ev}
	}
}

func (m LogModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleResize(msg)
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case logEventMsg:
		return m.handleLogEvent(msg.event)
	case backfillDoneMsg:
		return m.handleBackfillDone(msg)
	case logDisconnectMsg:
		return m, tea.Quit
	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)
	}
	return m, nil
}

// backfillDoneMsg delivers the initial tail content of a file to the
// log model. Fired by backfillActiveTab when a tab is activated.
type backfillDoneMsg struct {
	content string
}

func (m LogModel) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	// Reserve 1 row for the tab header line.
	m.viewport.SetWidth(m.width)
	m.viewport.SetHeight(m.height - 1)
	return m, nil
}

func (m LogModel) handleBackfillDone(msg backfillDoneMsg) (tea.Model, tea.Cmd) {
	if msg.content != "" {
		m.appendContent(msg.content)
	}
	if m.following {
		m.viewport.GotoBottom()
	}
	return m, nil
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

func (m LogModel) handleLogEvent(ev proto.ServerEvent) (tea.Model, tea.Cmd) {
	switch e := ev.(type) {
	case proto.EvtSessionsChanged:
		m.currentSession = pickActiveSession(e.Sessions, e.ActiveWindowID)
		m.rebuildTabs(m.currentSession)
		if e.IsPreview {
			if idx, ok := m.tabIndexByLabel("INFO"); ok {
				m.activeTab = idx
				m.renderInfoTab()
				m.following = true
			}
		} else if m.activeTabIs("INFO") {
			m.renderInfoTab()
		}
	case proto.EvtPaneFocused:
		if e.Pane == mainPane {
			if idx, ok := m.tabIndexByLabel("TRANSCRIPT"); ok {
				cmd := m.switchToTabCmd(idx)
				if m.client != nil {
					return m, tea.Batch(m.listenEvents(), cmd)
				}
				return m, cmd
			}
		}
	case proto.EvtLogLine:
		// Push content from the daemon's FileRelay. Match by path
		// against the active tab. Content may be multi-line.
		tab := m.activeTabState()
		if tab != nil && tab.logPath == e.Path && e.Line != "" {
			m.appendContent(strings.TrimRight(e.Line, "\n"))
			if m.following {
				m.viewport.GotoBottom()
			}
		}
	case proto.EvtTranscriptLine:
		// Push transcript content. Match by session id (the
		// transcript tab is the first tab with TabKindTranscript).
		if m.currentSession != nil && m.currentSession.ID == e.SessionID && m.isTranscriptTab() && e.Line != "" {
			m.appendContent(strings.TrimRight(e.Line, "\n"))
			if m.following {
				m.viewport.GotoBottom()
			}
		}
	}
	if m.client != nil {
		return m, m.listenEvents()
	}
	return m, nil
}

func pickActiveSession(sessions []proto.SessionInfo, activeWID string) *proto.SessionInfo {
	if activeWID == "" {
		return nil
	}
	for i := range sessions {
		if sessions[i].WindowID == activeWID {
			s := sessions[i]
			return &s
		}
	}
	return nil
}

func (m LogModel) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	mouse := msg.Mouse()
	if mouse.Y == 0 && mouse.Button == tea.MouseLeft {
		cmd := m.switchToTabCmd(m.tabIndexAtX(mouse.X))
		return m, cmd
	}
	return m, nil
}

func (m *LogModel) rebuildTabs(current *proto.SessionInfo) {
	prev := make(map[string]*tabState, len(m.tabs))
	for _, t := range m.tabs {
		prev[t.label] = t
	}
	prevTranscript := ""
	if t, ok := prev["TRANSCRIPT"]; ok {
		prevTranscript = t.logPath
	}
	transcriptPath := transcriptPathFromView(current)
	sessionChanged := transcriptPath != prevTranscript

	m.tabs = buildTabList(prev, current, m.appLogPath)
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

// transcriptPathFromView returns the path of the first TRANSCRIPT-kind
// tab the driver declared, or "" when no transcript tab exists.
func transcriptPathFromView(current *proto.SessionInfo) string {
	if current == nil {
		return ""
	}
	for _, lt := range current.View.LogTabs {
		if lt.Kind == state.TabKindTranscript {
			return lt.Path
		}
	}
	return ""
}

// buildTabList assembles the ordered tab list, reusing entries from prev
// (and removing each reused entry from the map) when label, path and kind
// all match. Order: driver-declared LogTabs (in driver's order) → INFO →
// LOG. The driver opts-out of INFO via SessionView.SuppressInfo.
func buildTabList(prev map[string]*tabState, current *proto.SessionInfo, appLogPath string) []*tabState {
	reuseOrNew := func(label, path string, kind state.TabKind) *tabState {
		if t, ok := prev[label]; ok && t.logPath == path && t.kind == kind {
			delete(prev, label)
			return t
		}
		return &tabState{label: label, logPath: path, kind: kind}
	}
	var tabs []*tabState
	if current != nil {
		for _, lt := range current.View.LogTabs {
			tabs = append(tabs, reuseOrNew(lt.Label, lt.Path, lt.Kind))
		}
		if !current.View.SuppressInfo {
			tabs = append(tabs, reuseOrNew("INFO", "", tabKindInfo))
		}
	}
	return append(tabs, reuseOrNew("LOG", appLogPath, tabKindLog))
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
	tab := m.activeTabState()
	return tab != nil && tab.kind == tabKindLog
}

func (m *LogModel) isTranscriptTab() bool {
	tab := m.activeTabState()
	return tab != nil && tab.kind == state.TabKindTranscript
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
	m.viewport.SetContent(renderInfoContent(m.currentSession))
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

// switchToTabCmd switches to a new tab and returns a backfill command.
// Use this from Update handlers that need to return a tea.Cmd.
func (m *LogModel) switchToTabCmd(tab logTab) tea.Cmd {
	m.switchToTab(tab)
	return m.backfillActiveTab()
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
	if t.kind == tabKindInfo {
		m.renderInfoTab()
		m.following = true
		return
	}

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

// backfillActiveTab reads the tail of the currently active tab's file
// and delivers it as a backfillDoneMsg. Used on startup and tab switch
// to populate the viewport before push events start flowing.
func (m LogModel) backfillActiveTab() tea.Cmd {
	tab := m.activeTabState()
	if tab == nil || tab.logPath == "" || tab.kind == tabKindInfo {
		return nil
	}
	path := tab.logPath
	return func() tea.Msg {
		content, _ := readTailLines(path, initialBackfillLines)
		return backfillDoneMsg{content: content}
	}
}

// readTailLines reads the last n lines from a file, for initial
// backfill. Returns the content as a single string. Does not maintain
// any state — each call opens, reads, and closes the file.
func readTailLines(path string, n int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	off, err := seekToLastNLines(f, n)
	if err != nil {
		return "", err
	}
	if _, err := f.Seek(off, 0); err != nil {
		return "", err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(data), "\n"), nil
}

// File I/O helpers (seekToLastNLines / trimLines) live in log_io.go.


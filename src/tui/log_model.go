package tui

import (
	"encoding/json"
	"io"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/viewport"
	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/state"
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
	label       string
	logPath     string
	kind        state.TabKind
	rendererCfg json.RawMessage
	file        *os.File
	offset      int64
	buf         string
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
	renderer       state.TabRenderer
	currentSession *proto.SessionInfo
}

func NewLogModel(appLogPath string, client *proto.Client) LogModel {
	return LogModel{
		appLogPath: appLogPath,
		tabs: []*tabState{
			{label: "LOG", logPath: appLogPath, kind: tabKindLog},
		},
		client:    client,
		activeTab: 0,
		following: true,
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
	case tea.MouseWheelMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		m.following = m.viewport.AtBottom()
		return m, cmd
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
		sessionChanged := m.rebuildTabs(m.currentSession)
		if e.IsPreview {
			if idx, ok := m.tabIndexByLabel("INFO"); ok {
				m.activeTab = idx
				m.renderInfoTab()
				m.following = true
			}
		} else if m.activeTabIs("INFO") {
			m.renderInfoTab()
		}
		if sessionChanged {
			var cmds []tea.Cmd
			if m.client != nil {
				cmds = append(cmds, m.listenEvents())
			}
			cmds = append(cmds, m.backfillActiveTab())
			return m, tea.Batch(cmds...)
		}
	case proto.EvtPaneFocused:
		if e.Pane == mainPane {
			if idx := m.firstRenderedTabIndex(); idx >= 0 {
				cmd := m.switchToTabCmd(logTab(idx))
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
	case proto.EvtSessionFileLine:
		// Push session file content. Match by session id and active
		// tab kind.
		tab := m.activeTabState()
		if m.currentSession != nil && m.currentSession.ID == e.SessionID &&
			tab != nil && string(tab.kind) == e.Kind && e.Line != "" {
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

func (m *LogModel) rebuildTabs(current *proto.SessionInfo) bool {
	prev := make(map[string]*tabState, len(m.tabs))
	for _, t := range m.tabs {
		prev[t.label] = t
	}
	prevRendered := firstRenderedPath(prev)
	renderedPath := renderedTabPath(current)
	sessionChanged := renderedPath != prevRendered

	m.tabs = buildTabList(prev, current, m.appLogPath)
	for _, t := range prev {
		if t.file != nil {
			t.file.Close()
		}
	}
	if int(m.activeTab) >= len(m.tabs) {
		m.activeTab = 0
	}
	if sessionChanged && renderedPath != "" {
		m.activeTab = 0
		m.viewport.SetContent("")
		m.following = true
		m.rebuildRenderer(m.activeTabState())
	}
	return sessionChanged
}

// firstRenderedTabIndex returns the index of the first tab that has a
// registered TabRenderer, or -1 when none exists.
func (m *LogModel) firstRenderedTabIndex() int {
	for i, t := range m.tabs {
		if state.HasTabRenderer(t.kind) {
			return i
		}
	}
	return -1
}

// firstRenderedPath returns the logPath of the first tab in prev that
// has a registered TabRenderer, or "".
func firstRenderedPath(prev map[string]*tabState) string {
	for _, t := range prev {
		if state.HasTabRenderer(t.kind) {
			return t.logPath
		}
	}
	return ""
}

// renderedTabPath returns the path of the first driver-declared LogTab
// that has a registered TabRenderer, or "" when none exists.
func renderedTabPath(current *proto.SessionInfo) string {
	if current == nil {
		return ""
	}
	for _, lt := range current.View.LogTabs {
		if state.HasTabRenderer(lt.Kind) {
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
	reuseOrNew := func(label, path string, kind state.TabKind, cfg json.RawMessage) *tabState {
		if t, ok := prev[label]; ok && t.logPath == path && t.kind == kind {
			delete(prev, label)
			t.rendererCfg = cfg
			return t
		}
		return &tabState{label: label, logPath: path, kind: kind, rendererCfg: cfg}
	}
	var tabs []*tabState
	if current != nil {
		for _, lt := range current.View.LogTabs {
			tabs = append(tabs, reuseOrNew(lt.Label, lt.Path, lt.Kind, lt.RendererCfg))
		}
		if !current.View.SuppressInfo {
			tabs = append(tabs, reuseOrNew("INFO", "", tabKindInfo, nil))
		}
	}
	return append(tabs, reuseOrNew("LOG", appLogPath, tabKindLog, nil))
}

// rebuildRenderer creates a new TabRenderer via the registry for the
// active tab's Kind. Called when the active session changes.
func (m *LogModel) rebuildRenderer(tab *tabState) {
	if tab == nil {
		m.renderer = nil
		return
	}
	m.renderer = state.NewTabRenderer(tab.kind, tab.rendererCfg)
}

func (m *LogModel) isLogTab() bool {
	tab := m.activeTabState()
	return tab != nil && tab.kind == tabKindLog
}

func (m *LogModel) isRenderedTab() bool {
	return m.renderer != nil
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
	m.rebuildRenderer(t)
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
	if m.isRenderedTab() {
		styled = m.renderer.Append([]byte(newContent))
	} else if m.isLogTab() {
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


package tui

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/tools"
)

type listItem struct {
	isProject   bool
	project     string
	projectPath string
	session     *proto.SessionInfo
	rows        int
}

func (li *listItem) SetRows(rendered string) {
	li.rows = strings.Count(rendered, "\n") + 1
}

type Model struct {
	client   *proto.Client
	cfg      *config.Config
	registry *tools.Registry
	keys     KeyMap

	sessions   []proto.SessionInfo
	connectors []proto.ConnectorInfo
	items      []listItem
	cursor     int
	offset     int // first visible item index for scrolling
	folded     map[string]bool
	filter     statusFilter
	active     string
	anchored   string
	mouseSeq   int
	hovering   bool
	lastMouseX int
	lastMouseY int
	width      int
	height     int
}

type serverEventMsg struct{ event proto.ServerEvent }
type disconnectMsg struct{}

type previewDoneMsg struct {
	sessionID string
	err       error
}

type switchDoneMsg struct {
	sessionID string
	err       error
}

type deactivateDoneMsg struct {
	err error
}

func NewModel(client *proto.Client, cfg *config.Config) Model {
	return Model{
		client:   client,
		cfg:      cfg,
		registry: tools.DefaultRegistry(),
		keys:     DefaultKeyMap(),
		folded:   make(map[string]bool),
		filter:   allOnFilter(),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.requestSessions(),
		m.listenEvents(),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case disconnectMsg:
		return m, tea.Quit

	case serverEventMsg:
		return m.handleServerEvent(msg.event)

	case previewDoneMsg:
		if msg.err == nil && msg.sessionID != "" {
			m.active = msg.sessionID
		}
		return m, m.focusCmd(sidebarPane)

	case switchDoneMsg:
		if msg.err == nil && msg.sessionID != "" {
			m.active = msg.sessionID
			m.anchored = msg.sessionID
		}
		return m, m.focusCmd(mainPane)

	case deactivateDoneMsg:
		if msg.err == nil {
			m.active = ""
			m.anchored = ""
		}
		return m, m.focusCmd(mainPane)

	case mouseLeaveMsg:
		return m.handleMouseLeave(msg)
	case tea.MouseWheelMsg:
		return m.handleMouseWheel(msg)
	case tea.MouseMotionMsg:
		return m.handleMouseMotion(msg)
	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, nil
		}
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleServerEvent(ev proto.ServerEvent) (tea.Model, tea.Cmd) {
	switch e := ev.(type) {
	case proto.EvtSessionsChanged:
		m.sessions = e.Sessions
		m.connectors = e.Connectors
		m.rebuildItems()
		if e.ActiveSessionID != "" && e.ActiveSessionID != m.active {
			m.active = e.ActiveSessionID
			m.anchored = e.ActiveSessionID
			if sc := m.findSessionCursorByID(e.ActiveSessionID); sc >= 0 {
				m.cursor = sc
			}
			return m, tea.Batch(m.listenEvents(), m.focusCmd(mainPane))
		}
		if e.ActiveSessionID == "" && m.active == "" {
			m.cursor = m.firstSessionIndex()
		}
	}
	return m, m.listenEvents()
}

// --- tea.Cmd wrappers ---

func (m Model) requestSessions() tea.Cmd {
	return func() tea.Msg {
		sessions, activeID, connectors, err := m.client.ListSessions()
		if err != nil {
			return nil
		}
		return serverEventMsg{event: proto.EvtSessionsChanged{
			Sessions:        sessions,
			ActiveSessionID: activeID,
			Connectors:      connectors,
		}}
	}
}

func (m Model) listenEvents() tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-m.client.Events()
		if !ok {
			return disconnectMsg{}
		}
		return serverEventMsg{event: ev}
	}
}

func (m Model) previewCmd(sess *proto.SessionInfo) tea.Cmd {
	return func() tea.Msg {
		activeID, err := m.client.PreviewSession(sess.ID)
		return previewDoneMsg{sessionID: activeID, err: err}
	}
}

func (m Model) switchCmd(sess *proto.SessionInfo) tea.Cmd {
	return func() tea.Msg {
		activeID, err := m.client.SwitchSession(sess.ID)
		return switchDoneMsg{sessionID: activeID, err: err}
	}
}

func (m Model) cursorPreviewCmd() tea.Cmd {
	if s := m.cursorSession(); s != nil && s.ID != m.active {
		return m.previewCmd(s)
	}
	return nil
}

func (m Model) anchoredPreviewCmd() tea.Cmd {
	idx := m.findSessionCursorByID(m.anchored)
	if idx < 0 {
		return nil
	}
	return m.previewCmd(m.items[idx].session)
}

func (m Model) launchToolCmd(toolName string, args map[string]string) tea.Cmd {
	return func() tea.Msg {
		_ = m.client.LaunchTool(toolName, args)
		return nil
	}
}

func (m Model) deactivateCmd() tea.Cmd {
	return func() tea.Msg {
		err := m.client.PreviewProject("")
		return deactivateDoneMsg{err: err}
	}
}

func (m Model) focusCmd(pane string) tea.Cmd {
	return func() tea.Msg {
		_ = m.client.FocusPane(pane)
		return nil
	}
}

// --- list building ---

func (m *Model) rebuildItems() {
	var prev listItem
	if m.cursor >= 0 && m.cursor < len(m.items) {
		prev = m.items[m.cursor]
	}

	byProject := make(map[string][]proto.SessionInfo)
	allProjects := make(map[string]string)

	for i := range m.sessions {
		s := &m.sessions[i]
		if !m.filter.matches(s.State) {
			continue
		}
		name := s.Name()
		byProject[name] = append(byProject[name], *s)
		allProjects[name] = s.Project
	}
	names := make([]string, 0, len(allProjects))
	for name := range allProjects {
		names = append(names, name)
	}
	sort.Strings(names)

	m.items = m.items[:0]
	for _, name := range names {
		path := allProjects[name]
		m.items = append(m.items, listItem{isProject: true, project: name, projectPath: path})
		if !m.folded[name] {
			for i := range byProject[name] {
				s := &byProject[name][i]
				m.items = append(m.items, listItem{project: name, projectPath: path, session: s})
			}
		}
	}

	m.restoreCursor(prev)
}

func (m *Model) restoreCursor(prev listItem) {
	if prev.session != nil {
		for i, item := range m.items {
			if item.session != nil && item.session.ID == prev.session.ID {
				m.cursor = i
				return
			}
		}
		for i, item := range m.items {
			if item.project == prev.project {
				m.cursor = i
				return
			}
		}
	} else if prev.isProject {
		for i, item := range m.items {
			if item.isProject && item.project == prev.project {
				m.cursor = i
				return
			}
		}
	}
	if len(m.items) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(m.items) {
		m.cursor = len(m.items) - 1
	} else if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m Model) totalItemRows() int {
	rows := 0
	for _, item := range m.items {
		rows += item.rows
	}
	return rows
}

// ensureCursorVisible adjusts m.offset so that the cursor item fits within
// bodyHeight rows. Items must already have their .rows set.
func (m *Model) ensureCursorVisible(bodyHeight int) {
	if len(m.items) == 0 || bodyHeight <= 0 {
		m.offset = 0
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	// Accumulate rows from offset to cursor. If they exceed bodyHeight,
	// advance offset by subtracting the front item (O(n) single pass).
	rows := 0
	for i := m.offset; i <= m.cursor && i < len(m.items); i++ {
		rows += m.items[i].rows
	}
	for rows > bodyHeight && m.offset < m.cursor {
		rows -= m.items[m.offset].rows
		m.offset++
	}
}

// visibleEnd returns the exclusive end index of items that fit within
// bodyHeight rows starting from m.offset.
func (m Model) visibleEnd(bodyHeight int) int {
	rows := 0
	for i := m.offset; i < len(m.items); i++ {
		if rows+m.items[i].rows > bodyHeight {
			return i
		}
		rows += m.items[i].rows
	}
	return len(m.items)
}

func (m Model) rowToItemIndex(y int) int {
	row := m.headerRowCount()
	if m.offset > 0 {
		row++ // "↑ N more" indicator line
	}
	sticky := stickyProject(m.items, m.offset)
	if sticky != "" {
		if y == row {
			return m.findProjectHeader(sticky)
		}
		row++ // sticky project header line
	}
	for i := m.offset; i < len(m.items); i++ {
		item := m.items[i]
		if item.rows <= 0 {
			continue
		}
		if y >= row && y < row+item.rows {
			return i
		}
		row += item.rows
	}
	return -1
}

func (m Model) findProjectHeader(name string) int {
	for i, item := range m.items {
		if item.isProject && item.project == name {
			return i
		}
	}
	return -1
}

// --- cursor helpers ---

func (m Model) cursorSession() *proto.SessionInfo {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return nil
	}
	return m.items[m.cursor].session
}

func (m Model) cursorProjectPath() string {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return ""
	}
	return m.items[m.cursor].projectPath
}

func (m Model) cursorProjectName() string {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return ""
	}
	return m.items[m.cursor].project
}

func (m Model) findSessionCursorByID(id string) int {
	for i, item := range m.items {
		if item.session != nil && item.session.ID == id {
			return i
		}
	}
	return -1
}

// headerRowCount returns the number of rendered rows before the first
// list item. Accounts for the optional connector summary line.
func (m Model) headerRowCount() int {
	n := 3 // header + filter bar + blank
	if m.hasConnectorSummary() {
		n++
	}
	return n
}

func (m Model) hasConnectorSummary() bool {
	return m.connectorSummaryLine() != ""
}

// connectorSummaryLine returns the combined summary text from all
// available connectors, or "" if none.
func (m Model) connectorSummaryLine() string {
	if len(m.connectors) == 0 {
		return ""
	}
	var parts []string
	for _, c := range m.connectors {
		if c.Summary != "" {
			parts = append(parts, c.Summary)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}

func (m Model) firstSessionIndex() int {
	for i, item := range m.items {
		if !item.isProject {
			return i
		}
	}
	return 0
}

package tui

import (
	"log/slog"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/key"
	"github.com/take/agent-roost/config"
	"github.com/take/agent-roost/core"
	"github.com/take/agent-roost/session/driver"
)

type listItem struct {
	isProject   bool
	project     string
	projectPath string
	session     *core.SessionInfo
	rows        int
}

func (li *listItem) SetRows(rendered string) {
	li.rows = strings.Count(rendered, "\n") + 1
}

type Model struct {
	client   *core.Client
	cfg      *config.Config
	registry *core.ToolRegistry
	drivers  *driver.Registry
	keys     KeyMap

	sessions []core.SessionInfo
	items    []listItem // for rendering (project rows + session rows)
	cursor   int        // index into items
	folded   map[string]bool
	active   string
	anchored string
	mouseSeq int
	hovering bool
	lastMouseX int
	lastMouseY int
	width    int
	height   int
}

type serverEventMsg core.Message
type disconnectMsg struct{}
type mouseLeaveMsg struct{ seq int }

const mouseLeaveTimeout = 200 * time.Millisecond

type previewDoneMsg struct {
	windowID string
	err      error
}

type switchDoneMsg struct {
	windowID string
	err      error
}

func NewModel(client *core.Client, cfg *config.Config) Model {
	return Model{
		client:   client,
		cfg:      cfg,
		registry: core.DefaultToolRegistry(),
		drivers:  driver.DefaultRegistry(),
		keys:     DefaultKeyMap(),
		folded:   make(map[string]bool),
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
		return m.handleServerEvent(core.Message(msg))

	case previewDoneMsg:
		if msg.err == nil && msg.windowID != "" {
			m.active = msg.windowID
		}
		return m, m.focusCmd("0.2")

	case switchDoneMsg:
		if msg.err == nil && msg.windowID != "" {
			m.active = msg.windowID
			m.anchored = msg.windowID
		}
		return m, m.focusCmd("0.0")

	case mouseLeaveMsg:
		return m.handleMouseLeave(msg)
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

func (m Model) handleMouseLeave(msg mouseLeaveMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.mouseSeq || !m.isMouseAtEdge() {
		return m, nil
	}
	m.hovering = false
	if m.anchored == "" || m.anchored == m.active {
		return m, nil
	}
	idx := m.findSessionCursorByWindowID(m.anchored)
	if idx < 0 {
		m.anchored = ""
		return m, nil
	}
	m.cursor = idx
	return m, m.anchoredPreviewCmd()
}

func (m Model) handleMouseMotion(msg tea.MouseMotionMsg) (tea.Model, tea.Cmd) {
	m.hovering = true
	m.mouseSeq++
	seq := m.mouseSeq
	leaveTimer := tea.Tick(mouseLeaveTimeout, func(time.Time) tea.Msg {
		return mouseLeaveMsg{seq: seq}
	})
	mouse := msg.Mouse()
	m.lastMouseX = mouse.X
	m.lastMouseY = mouse.Y
	idx := m.rowToItemIndex(mouse.Y)
	if idx < 0 || idx == m.cursor {
		return m, leaveTimer
	}
	m.cursor = idx
	if cmd := m.cursorPreviewCmd(); cmd != nil {
		return m, tea.Batch(cmd, leaveTimer)
	}
	return m, leaveTimer
}

func (m Model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	mouse := msg.Mouse()
	if mouse.Button != tea.MouseLeft {
		return m, nil
	}
	idx := m.rowToItemIndex(mouse.Y)
	if idx < 0 {
		return m, nil
	}
	if m.items[idx].isProject {
		name := m.items[idx].project
		m.folded[name] = !m.folded[name]
		m.rebuildItems()
		return m, nil
	}
	m.cursor = idx
	if s := m.cursorSession(); s != nil {
		m.anchored = s.WindowID
		return m, m.focusCmd("0.0")
	}
	return m, nil
}

func (m Model) handleServerEvent(msg core.Message) (tea.Model, tea.Cmd) {
	switch msg.Event {
	case "sessions-changed":
		m.sessions = msg.Sessions
		m.rebuildItems()
		if msg.ActiveWindowID != "" && msg.ActiveWindowID != m.active {
			m.active = msg.ActiveWindowID
			m.anchored = msg.ActiveWindowID
			if sc := m.findSessionCursorByWindowID(msg.ActiveWindowID); sc >= 0 {
				m.cursor = sc
			}
			return m, tea.Batch(m.listenEvents(), m.focusCmd("0.0"))
		}
		if msg.ActiveWindowID == "" && m.active == "" {
			m.cursor = m.firstSessionIndex()
		}
	case "states-updated":
		for i := range m.sessions {
			if st, ok := msg.States[m.sessions[i].WindowID]; ok {
				m.sessions[i].State = st
			}
		}
		m.rebuildItems()
	}
	return m, m.listenEvents()
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.hovering = false
	switch {
	case key.Matches(msg, m.keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
		if s := m.cursorSession(); s != nil {
			m.anchored = s.WindowID
		}
		return m, m.cursorPreviewCmd()
	case key.Matches(msg, m.keys.Down):
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
		if s := m.cursorSession(); s != nil {
			m.anchored = s.WindowID
		}
		return m, m.cursorPreviewCmd()
	case key.Matches(msg, m.keys.Enter):
		if s := m.cursorSession(); s != nil {
			m.anchored = s.WindowID
			return m, m.focusCmd("0.0")
		}
	case key.Matches(msg, m.keys.New):
		return m, m.launchToolCmd("new-session", map[string]string{
			"project": m.cursorProjectPath(),
			"command": m.cfg.Session.DefaultCommand,
		})
	case key.Matches(msg, m.keys.NewCmd):
		return m, m.launchToolCmd("new-session", map[string]string{
			"project": m.cursorProjectPath(),
		})
	case key.Matches(msg, m.keys.Stop):
		if s := m.cursorSession(); s != nil {
			return m, m.launchToolCmd("stop-session", map[string]string{
				"session_id": s.ID,
			})
		}
	case key.Matches(msg, m.keys.Toggle):
		name := m.cursorProjectName()
		if name != "" {
			m.folded[name] = !m.folded[name]
			m.rebuildItems()
		}
	}
	return m, nil
}

// --- tea.Cmd wrappers ---

func (m Model) requestSessions() tea.Cmd {
	return func() tea.Msg {
		sessions, activeWID, err := m.client.ListSessions()
		if err != nil {
			slog.Error("list-sessions failed", "err", err)
			return nil
		}
		msg := core.NewEvent("sessions-changed")
		msg.Sessions = sessions
		msg.ActiveWindowID = activeWID
		return serverEventMsg(msg)
	}
}

func (m Model) listenEvents() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-m.client.Events()
		if !ok {
			return disconnectMsg{}
		}
		return serverEventMsg(msg)
	}
}

func (m Model) previewCmd(sess *core.SessionInfo) tea.Cmd {
	return func() tea.Msg {
		activeWID, err := m.client.PreviewSession(sess.ID)
		return previewDoneMsg{windowID: activeWID, err: err}
	}
}

func (m Model) switchCmd(sess *core.SessionInfo) tea.Cmd {
	return func() tea.Msg {
		activeWID, err := m.client.SwitchSession(sess.ID)
		return switchDoneMsg{windowID: activeWID, err: err}
	}
}

func (m Model) cursorPreviewCmd() tea.Cmd {
	if s := m.cursorSession(); s != nil && s.WindowID != m.active {
		return m.previewCmd(s)
	}
	return nil
}

func (m Model) anchoredPreviewCmd() tea.Cmd {
	idx := m.findSessionCursorByWindowID(m.anchored)
	if idx < 0 {
		return nil
	}
	return m.previewCmd(m.items[idx].session)
}

const edgeMargin = 3

func (m Model) isMouseAtEdge() bool {
	return m.lastMouseX < edgeMargin || m.lastMouseX >= m.width-edgeMargin ||
		m.lastMouseY < edgeMargin || m.lastMouseY >= m.height-edgeMargin
}

func (m Model) launchToolCmd(toolName string, args map[string]string) tea.Cmd {
	return func() tea.Msg {
		m.client.LaunchTool(toolName, args)
		return nil
	}
}

func (m Model) focusCmd(pane string) tea.Cmd {
	return func() tea.Msg {
		m.client.FocusPane(pane)
		return nil
	}
}

// --- list building ---

func (m *Model) rebuildItems() {
	var prev listItem
	if m.cursor >= 0 && m.cursor < len(m.items) {
		prev = m.items[m.cursor]
	}

	byProject := make(map[string][]core.SessionInfo)
	allProjects := make(map[string]string)

	for i := range m.sessions {
		s := &m.sessions[i]
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

	restored := false
	if prev.session != nil {
		for i, item := range m.items {
			if item.session != nil && item.session.WindowID == prev.session.WindowID {
				m.cursor = i
				restored = true
				break
			}
		}
	} else if prev.isProject {
		for i, item := range m.items {
			if item.isProject && item.project == prev.project {
				m.cursor = i
				restored = true
				break
			}
		}
	}
	if !restored && len(m.items) > 0 && m.cursor >= len(m.items) {
		m.cursor = len(m.items) - 1
	}
}

// rowToItemIndex maps a terminal row Y (0-based) to an item index.
// Row counts are cached per item by SetRows during View rendering.
// Returns -1 if outside items.
func (m Model) rowToItemIndex(y int) int {
	const headerRows = 2
	row := headerRows
	for i, item := range m.items {
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

// --- cursor helpers ---

func (m Model) cursorSession() *core.SessionInfo {
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

func (m Model) hasWindowID(wid string) bool {
	for _, s := range m.sessions {
		if s.WindowID == wid {
			return true
		}
	}
	return false
}

func (m Model) findSessionCursorByWindowID(wid string) int {
	for i, item := range m.items {
		if item.session != nil && item.session.WindowID == wid {
			return i
		}
	}
	return -1
}

func (m Model) firstSessionIndex() int {
	for i, item := range m.items {
		if !item.isProject {
			return i
		}
	}
	return 0
}

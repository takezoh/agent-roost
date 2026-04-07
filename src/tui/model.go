package tui

import (
	"log/slog"
	"sort"
	"strings"

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
	items    []listItem            // 描画・マウスY変換用（プロジェクト行含む）
	visible  []*core.SessionInfo   // 表示中のセッション一覧（cursor はここのインデックス）
	cursor   int
	folded   map[string]bool
	active   string
	width    int
	height   int
}

type serverEventMsg core.Message
type disconnectMsg struct{}

type previewDoneMsg struct {
	windowID string
	err      error
}

type switchDoneMsg struct {
	windowID string
	err      error
}

type previewProjectDoneMsg struct {
	err error
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
		}
		return m, m.focusCmd("0.0")

	case previewProjectDoneMsg:
		if msg.err == nil {
			m.active = ""
		}
		return m, m.focusCmd("0.2")

	case tea.MouseMotionMsg:
		mouse := msg.Mouse()
		idx := m.rowToItemIndex(mouse.Y)
		if idx < 0 || m.items[idx].isProject {
			return m, nil
		}
		sc := m.itemToSessionCursor(idx)
		if sc < 0 || sc == m.cursor {
			return m, nil
		}
		m.cursor = sc
		return m, m.cursorPreviewCmd()

	case tea.MouseClickMsg:
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
		sc := m.itemToSessionCursor(idx)
		if sc < 0 {
			return m, nil
		}
		m.cursor = sc
		if m.cursorSession() != nil {
			return m, m.focusCmd("0.0")
		}
		return m, nil

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, nil
		}
		return m.handleKey(msg)
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
			if sc := m.findSessionCursorByWindowID(msg.ActiveWindowID); sc >= 0 {
				m.cursor = sc
			}
			return m, tea.Batch(m.listenEvents(), m.focusCmd("0.0"))
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
	switch {
	case key.Matches(msg, m.keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
		return m, m.cursorPreviewCmd()
	case key.Matches(msg, m.keys.Down):
		if m.cursor < len(m.visible)-1 {
			m.cursor++
		}
		return m, m.cursorPreviewCmd()
	case key.Matches(msg, m.keys.Enter):
		if m.cursorSession() != nil {
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
	if m.cursorSession() == nil && m.active != "" {
		if project := m.cursorProjectPath(); project != "" {
			return m.previewProjectCmd(project)
		}
	}
	return nil
}

func (m Model) previewProjectCmd(project string) tea.Cmd {
	return func() tea.Msg {
		err := m.client.PreviewProject(project)
		return previewProjectDoneMsg{err: err}
	}
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
	m.visible = m.visible[:0]
	for _, name := range names {
		path := allProjects[name]
		m.items = append(m.items, listItem{isProject: true, project: name, projectPath: path})
		if !m.folded[name] {
			for i := range byProject[name] {
				s := &byProject[name][i]
				m.items = append(m.items, listItem{project: name, projectPath: path, session: s})
				m.visible = append(m.visible, s)
			}
		}
	}
	if m.cursor >= len(m.visible) && len(m.visible) > 0 {
		m.cursor = len(m.visible) - 1
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

// itemToSessionCursor maps an items index to a cursor (visible session) index.
// Returns -1 if the item is a project row.
func (m Model) itemToSessionCursor(idx int) int {
	count := 0
	for i, item := range m.items {
		if item.isProject {
			continue
		}
		if i == idx {
			return count
		}
		count++
	}
	return -1
}

// --- cursor helpers ---

func (m Model) cursorSession() *core.SessionInfo {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return nil
	}
	return m.visible[m.cursor]
}

func (m Model) cursorProjectPath() string {
	if s := m.cursorSession(); s != nil {
		return s.Project
	}
	return ""
}

func (m Model) cursorProjectName() string {
	if s := m.cursorSession(); s != nil {
		return s.Name()
	}
	return ""
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
	for i, s := range m.visible {
		if s.WindowID == wid {
			return i
		}
	}
	return -1
}

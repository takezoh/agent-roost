package tui

import (
	"encoding/json"
	"io"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/state"
)

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
	if sessionChanged {
		m.activeTab = 0
		m.viewport.SetContent("")
		m.following = true
		m.rebuildRenderer(m.activeTabState())
	}
	return sessionChanged
}

func (m *LogModel) firstRenderedTabIndex() int {
	for i, t := range m.tabs {
		if state.HasTabRenderer(t.kind) {
			return i
		}
	}
	return -1
}

func firstRenderedPath(prev map[string]*tabState) string {
	for _, t := range prev {
		if state.HasTabRenderer(t.kind) {
			return t.logPath
		}
	}
	return ""
}

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
			// TODO: prefix with glyphs.Get("tab.info") once label rendering
			// supports styled prefixes without breaking tab hit-testing.
			tabs = append(tabs, reuseOrNew("INFO", "", tabKindInfo, nil))
		}
	}
	// TODO: prefix with glyphs.Get("tab.log") — same constraint as above.
	return append(tabs, reuseOrNew("LOG", appLogPath, tabKindLog, nil))
}

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
	m.viewport.SetContent(renderInfoContent(m.currentSession, m.width))
}

func (m *LogModel) activeTabState() *tabState {
	idx := int(m.activeTab)
	if idx >= 0 && idx < len(m.tabs) {
		return m.tabs[idx]
	}
	return nil
}

func (m *LogModel) switchToTabCmd(tab logTab) tea.Cmd {
	if !m.switchToTab(tab) {
		return nil
	}
	return m.backfillActiveTab()
}

func (m *LogModel) switchToTab(tab logTab) bool {
	if tab == m.activeTab {
		return false
	}
	m.activeTab = tab

	t := m.activeTabState()
	if t == nil {
		return false
	}
	if t.kind == tabKindInfo {
		m.renderInfoTab()
		m.following = true
		return true
	}

	m.viewport.SetContent("")
	m.following = true
	m.rebuildRenderer(t)
	return true
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
	content := styled
	if existing != "" {
		content = existing + "\n" + styled
	}
	m.viewport.SetContent(trimLines(content, maxLogLines))
}

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

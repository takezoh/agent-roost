package transcript

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Tracker incrementally parses Claude transcript files for one or more
// agent sessions and produces a status-line snapshot describing each.
// It implements the core.SessionTracker interface but lives here so the
// core package stays driver-agnostic.
type Tracker struct {
	mu       sync.Mutex
	sessions map[string]*trackerState
}

type trackerState struct {
	parser       *Parser
	offset       int64
	buf          string
	insight      SessionInsight
	model        string
	inputTokens  int
	outputTokens int
	lastLine     string
}

func NewTracker() *Tracker {
	return &Tracker{sessions: make(map[string]*trackerState)}
}

// Update folds any new content appended to transcriptPath into the
// cached state and returns the formatted status line.
func (t *Tracker) Update(agentSessionID, transcriptPath string) (string, bool) {
	if transcriptPath == "" || agentSessionID == "" {
		return "", false
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	st := t.stateFor(agentSessionID)
	if err := st.scanNewLines(transcriptPath); err != nil {
		return st.lastLine, false
	}
	newLine := FormatStatusLine(st.snapshot())
	changed := newLine != st.lastLine
	st.lastLine = newLine
	return newLine, changed
}

func (t *Tracker) stateFor(id string) *trackerState {
	if st, ok := t.sessions[id]; ok {
		return st
	}
	st := &trackerState{parser: NewParser(ParserOptions{})}
	t.sessions[id] = st
	return st
}

func (st *trackerState) scanNewLines(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if st.offset > 0 {
		if _, err := f.Seek(st.offset, 0); err != nil {
			return err
		}
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 1024*1024)
	first := true
	for scanner.Scan() {
		line := scanner.Bytes()
		if first && st.buf != "" {
			line = append([]byte(st.buf), line...)
			st.buf = ""
		}
		first = false
		st.applyLine(line)
	}
	if pos, err := f.Seek(0, 1); err == nil {
		st.offset = pos
	}
	return nil
}

func (st *trackerState) applyLine(line []byte) {
	if u := ParseTurnUsage(line); u != nil {
		st.model = u.Model
		st.inputTokens += u.TotalInputTokens()
		st.outputTokens += u.OutputTokens
	}
	UpdateInsight(&st.insight, st.parser.ParseLines(line))
}

func (st *trackerState) snapshot() StatusSnapshot {
	return StatusSnapshot{
		Model:        st.model,
		InputTokens:  st.inputTokens,
		OutputTokens: st.outputTokens,
		Insight:      st.insight,
	}
}

// StatusSnapshot is the per-session view consumed by FormatStatusLine.
type StatusSnapshot struct {
	Model        string
	InputTokens  int
	OutputTokens int
	Insight      SessionInsight
}

// FormatStatusLine renders a tmux status-line string from a snapshot.
// Sections are separated by " | " and omitted when empty.
func FormatStatusLine(snap StatusSnapshot) string {
	var parts []string
	if snap.Model != "" {
		parts = append(parts, snap.Model)
	}
	if snap.InputTokens > 0 || snap.OutputTokens > 0 {
		parts = append(parts, fmt.Sprintf("%s↓ %s↑", formatTokens(snap.InputTokens), formatTokens(snap.OutputTokens)))
	}
	if snap.Insight.CurrentTool != "" {
		parts = append(parts, "▸ "+snap.Insight.CurrentTool)
	}
	if snap.Insight.ErrorCount > 0 {
		parts = append(parts, fmt.Sprintf("%d err", snap.Insight.ErrorCount))
	}
	if n := snap.Insight.SubagentTotal(); n > 0 {
		parts = append(parts, fmt.Sprintf("%d subs", n))
	}
	return strings.Join(parts, " | ")
}

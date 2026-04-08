package core

import (
	"bufio"
	"os"
	"sync"
)

// TurnUsage holds per-turn usage data parsed from a transcript line.
type TurnUsage struct {
	Model        string
	InputTokens  int
	OutputTokens int
}

// TurnUsageParser parses a single transcript JSONL line into TurnUsage.
// Returns nil if the line contains no usage data.
type TurnUsageParser func(line []byte) *TurnUsage

// StatusLineFormatter formats model and token counts into a status line string.
type StatusLineFormatter func(model string, inputTokens, outputTokens int) string

// UsageTracker accumulates transcript usage per agent session.
type UsageTracker struct {
	mu       sync.Mutex
	parse    TurnUsageParser
	format   StatusLineFormatter
	sessions map[string]*usageState
}

type usageState struct {
	offset       int64
	buf          string
	model        string
	inputTokens  int
	outputTokens int
	lastLine     string
}

func NewUsageTracker() *UsageTracker {
	return &UsageTracker{sessions: make(map[string]*usageState)}
}

// SetFuncs sets the parse and format functions.
func (t *UsageTracker) SetFuncs(parse TurnUsageParser, format StatusLineFormatter) {
	t.parse = parse
	t.format = format
}

// Update reads new transcript content and returns the formatted status line.
func (t *UsageTracker) Update(agentSessionID, transcriptPath string) (string, bool) {
	if transcriptPath == "" || t.parse == nil {
		return "", false
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	st, ok := t.sessions[agentSessionID]
	if !ok {
		st = &usageState{}
		t.sessions[agentSessionID] = st
	}

	f, err := os.Open(transcriptPath)
	if err != nil {
		return st.lastLine, false
	}
	defer f.Close()

	if st.offset > 0 {
		if _, err := f.Seek(st.offset, 0); err != nil {
			return st.lastLine, false
		}
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if first && st.buf != "" {
			line = st.buf + line
			st.buf = ""
		}
		first = false
		u := t.parse([]byte(line))
		if u == nil {
			continue
		}
		st.model = u.Model
		st.inputTokens += u.InputTokens
		st.outputTokens += u.OutputTokens
	}

	pos, err := f.Seek(0, 1)
	if err == nil {
		st.offset = pos
	}

	newLine := t.format(st.model, st.inputTokens, st.outputTokens)
	changed := newLine != st.lastLine
	st.lastLine = newLine
	return newLine, changed
}

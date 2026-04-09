package transcript

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Tracker incrementally parses Claude transcript files for one or more
// agent sessions and produces a status-line snapshot describing each. It
// is the single window through which Drivers consume transcripts: every
// piece of cached state (title / lastPrompt / subjects / insight / token
// counters) is folded in via the offset-based scanner, so the same JSONL
// file is never re-parsed from scratch on every tick.
//
// Tracker is NOT thread-safe. Each Driver owns its own Tracker instance
// and accesses it from a single goroutine (the driverActor run loop in
// production, the test goroutine in unit tests). Concurrent use across
// goroutines will race.
type Tracker struct {
	sessions map[string]*trackerState
}

type trackerState struct {
	parser *Parser
	offset int64
	buf    string

	// meta — title is last-write-wins from KindCustomTitle.
	title string
	// subjects accumulates TaskCreate primaries (deduplicated, capped).
	subjects []string

	// Active conversation chain. parentOf records every conversation entry
	// (user + assistant) so chain walks can hop through assistant turns.
	// userPrompts holds the text of KindUser entries with non-empty content,
	// keyed by their UUID. tailUUID is the most recently appended entry.
	//
	// activeLastPrompt() walks parentOf backwards from tailUUID and returns
	// the text of the first KindUser found — rewound branches are not
	// reachable from the new tail, so resubmitted prompts naturally win
	// over the original ones.
	parentOf    map[string]string
	userPrompts map[string]string
	tailUUID    string

	// insight accumulates tool/subagent/error counts incrementally.
	insight SessionInsight

	// status-line metrics
	model        string
	inputTokens  int
	outputTokens int

	// formattedLine caches the most recent FormatStatusLine output so that
	// callers asking for "did the line change?" can compare cheaply.
	formattedLine string
}

func NewTracker() *Tracker {
	return &Tracker{sessions: make(map[string]*trackerState)}
}

// Update folds any new content appended to transcriptPath into the
// cached per-session state. Returns whether the rendered status line
// would now differ from the previous one (cheap change-detection for
// callers that don't always need the formatted string).
func (t *Tracker) Update(agentSessionID, transcriptPath string) (changed bool, err error) {
	if transcriptPath == "" || agentSessionID == "" {
		return false, nil
	}
	st := t.stateFor(agentSessionID)
	if err := st.scanNewLines(transcriptPath); err != nil {
		return false, err
	}
	newLine := FormatStatusLine(st.snapshot())
	changed = newLine != st.formattedLine
	st.formattedLine = newLine
	return changed, nil
}

// StatusLine returns the cached formatted status line for the given
// session. Empty string for unknown sessions or before the first Update.
func (t *Tracker) StatusLine(agentSessionID string) string {
	st, ok := t.sessions[agentSessionID]
	if !ok {
		return ""
	}
	return st.formattedLine
}

// Snapshot returns the current cached MetaSnapshot for the session. The
// returned slices are copies — callers may mutate them freely.
func (t *Tracker) Snapshot(agentSessionID string) MetaSnapshot {
	st, ok := t.sessions[agentSessionID]
	if !ok {
		return MetaSnapshot{}
	}
	subjects := append([]string(nil), st.subjects...)
	return MetaSnapshot{
		Title:      st.title,
		LastPrompt: st.activeLastPrompt(),
		Subjects:   subjects,
		Insight:    st.insight,
	}
}

// activeLastPrompt walks parentOf backwards from tailUUID and returns the
// text of the first KindUser entry encountered. Rewound branches are not
// reachable from the current tail because resubmits start a new branch
// off the same parent (verified empirically against real transcripts), so
// the older user-prompt siblings are naturally skipped.
//
// The bounded loop guards against pathological cycles — real Claude
// transcripts form a DAG, but cheap insurance is worth one comparison.
func (st *trackerState) activeLastPrompt() string {
	const maxWalk = 100000
	cur := st.tailUUID
	for i := 0; i < maxWalk && cur != ""; i++ {
		if text, ok := st.userPrompts[cur]; ok {
			return text
		}
		cur = st.parentOf[cur]
	}
	return ""
}

// Forget releases all per-session state. Drivers must call this from
// Close() so the Tracker doesn't grow unbounded over the process lifetime.
func (t *Tracker) Forget(agentSessionID string) {
	delete(t.sessions, agentSessionID)
}

func (t *Tracker) stateFor(id string) *trackerState {
	if st, ok := t.sessions[id]; ok {
		return st
	}
	st := &trackerState{
		parser:      NewParser(ParserOptions{}),
		parentOf:    make(map[string]string),
		userPrompts: make(map[string]string),
	}
	t.sessions[id] = st
	return st
}

func (st *trackerState) scanNewLines(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// File-truncation detection: if the file is now smaller than our
	// cached read offset, the underlying transcript was rewound (e.g. via
	// `claude --resume` rolling back to a prior turn). Reset all cached
	// state and re-parse from byte 0.
	if info, statErr := f.Stat(); statErr == nil {
		if info.Size() < st.offset {
			st.resetForRescan()
		}
	}

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

// resetForRescan clears all derived state but keeps the parser instance
// (its tool_use_id index will be repopulated as we re-scan).
func (st *trackerState) resetForRescan() {
	st.parser.Reset()
	st.offset = 0
	st.buf = ""
	st.title = ""
	st.subjects = st.subjects[:0]
	st.insight = SessionInsight{}
	st.model = ""
	st.inputTokens = 0
	st.outputTokens = 0
	for k := range st.parentOf {
		delete(st.parentOf, k)
	}
	for k := range st.userPrompts {
		delete(st.userPrompts, k)
	}
	st.tailUUID = ""
}

func (st *trackerState) applyLine(line []byte) {
	if u := ParseTurnUsage(line); u != nil {
		st.model = u.Model
		st.inputTokens += u.TotalInputTokens()
		st.outputTokens += u.OutputTokens
	}
	entries := st.parser.ParseLines(line)
	for _, e := range entries {
		st.applyMetaEntry(e)
		st.applyChainEntry(e)
	}
	UpdateInsight(&st.insight, entries)
}

// applyMetaEntry handles the title / subjects branches. Note that
// lastPrompt is NOT updated here — it's derived from the parentUuid
// chain via activeLastPrompt() so that rewound user prompts are
// transparently filtered out.
func (st *trackerState) applyMetaEntry(e Entry) {
	switch e.Kind {
	case KindCustomTitle:
		st.title = e.Text
	case KindToolUse:
		if e.ToolName == "TaskCreate" && e.ToolInput.Primary != "" {
			st.subjects = appendBoundedUnique(st.subjects, e.ToolInput.Primary, maxSubjectsAgg)
		}
	}
}

// applyChainEntry records uuid → parentUuid for any conversation entry,
// caches the prompt text on KindUser entries with non-empty content, and
// advances the tail. Non-conversation entries (system, attachment,
// custom-title, ...) carry no UUID in the wire format and are skipped.
// Synthetic KindUser entries (skill bootstrap, interrupt markers, etc.)
// extend the chain so subsequent walks still terminate, but their text
// is NOT registered as a candidate user prompt.
func (st *trackerState) applyChainEntry(e Entry) {
	if e.UUID == "" {
		return
	}
	st.parentOf[e.UUID] = e.ParentUUID
	if e.Kind == KindUser && e.Text != "" && !e.Synthetic {
		st.userPrompts[e.UUID] = e.Text
	}
	st.tailUUID = e.UUID
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

package vt

import (
	"fmt"
	"hash/fnv"
	"strings"

	xvt "github.com/charmbracelet/x/vt"
)

// PromptPhase classifies an OSC 133 semantic-prompt event.
type PromptPhase int

const (
	PromptPhaseNone    PromptPhase = iota
	PromptPhaseStart               // 133;A — prompt rendering started
	PromptPhaseInput               // 133;B — prompt done, awaiting input
	PromptPhaseCommand             // 133;C — command execution started
	PromptPhaseComplete            // 133;D — command finished
)

// PromptEvent is a single OSC 133 semantic-prompt event captured from the
// terminal stream.
type PromptEvent struct {
	Phase    PromptPhase
	ExitCode *int // non-nil only for PromptPhaseComplete (133;D;<exit-code>)
}

// Snapshot is the parsed state of a terminal emulator at a point in time.
type Snapshot struct {
	Cols, Rows    int
	CursorX, CursorY int
	AtAltScreen   bool
	// Stable is an FNV-64a hash of all visible cell content+link.
	// If it equals the previous Snapshot.Stable, DirtyCount is 0.
	Stable      string
	// LastLine is the last non-empty visible line, used for prompt detection.
	LastLine    string
	// DirtyCount is 0 when Stable matches the previous snapshot, 1 otherwise.
	DirtyCount  int
	Notifications []OscNotification
	// PromptEvents holds OSC 133 semantic-prompt events captured since the
	// previous Snapshot. Flushed (set to nil) on each Snapshot() call.
	PromptEvents []PromptEvent
}

// OscNotification is a desktop-notification request captured from an OSC
// 9 / 99 / 777 escape sequence emitted by an agent process.
type OscNotification struct {
	Cmd     int    // 9, 99, or 777
	Payload string // raw payload (leading ';' stripped)
}

// computeSnapshot builds a Snapshot from the current emulator state.
// prevStable is the Stable hash of the previous call (used for DirtyCount).
func computeSnapshot(em xvt.Terminal, prevStable string, notifs []OscNotification, promptEvents []PromptEvent) Snapshot {
	w := em.Width()
	h := em.Height()
	pos := em.CursorPosition()

	hf := fnv.New64a()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := em.CellAt(x, y)
			if c == nil {
				fmt.Fprint(hf, " ||") //nolint:errcheck
				continue
			}
			fmt.Fprintf(hf, "%s|%d|%s|", c.Content, c.Width, c.Link.URL) //nolint:errcheck
		}
		fmt.Fprint(hf, "\n") //nolint:errcheck
	}
	stable := fmt.Sprintf("%016x", hf.Sum64())

	dirty := 0
	if prevStable != "" && stable != prevStable {
		dirty = 1
	}

	return Snapshot{
		Cols:          w,
		Rows:          h,
		CursorX:       pos.X,
		CursorY:       pos.Y,
		AtAltScreen:   em.IsAltScreen(),
		Stable:        stable,
		LastLine:      lastNonEmptyLine(em),
		DirtyCount:    dirty,
		Notifications: notifs,
		PromptEvents:  promptEvents,
	}
}

func lastNonEmptyLine(em xvt.Terminal) string {
	h := em.Height()
	w := em.Width()
	for y := h - 1; y >= 0; y-- {
		var sb strings.Builder
		for x := 0; x < w; x++ {
			c := em.CellAt(x, y)
			if c != nil {
				sb.WriteString(c.Content)
			}
		}
		if s := strings.TrimRight(sb.String(), " "); s != "" {
			return s
		}
	}
	return ""
}

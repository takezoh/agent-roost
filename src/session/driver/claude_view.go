package driver

import (
	"fmt"
	"path/filepath"
)

// View constructs a SessionView snapshot from the driver's currently
// cached state. View() is a pure getter — it must not perform I/O or
// detection (those belong in Tick / HandleEvent).
//
// Card content:
//   - Title    = transcript title (set by refreshMeta)
//   - Subtitle = haiku-generated session summary, falling back to
//                lastPrompt while haiku is still computing or hasn't run
//                yet. lastPrompt is now seeded from the UserPromptSubmit
//                hook payload directly (HandleEvent), so it is populated
//                even on the first turn of a brand-new session before
//                Claude has flushed anything to JSONL.
//   - Tags     = [CommandTag("claude"), BranchTag(branch?)]
//   - Indicators = derived from transcript insight
//
// LogTabs:
//   - TRANSCRIPT (transcript) — when transcriptPath is known
//   - EVENTS (text)           — always declared (file may not exist yet,
//                              tail handles missing file gracefully)
//
// InfoExtras: driver-specific INFO additions (Title / Last Prompt / Tool /
// Subagents / Errors). The TUI prepends a generic header on its own.
//
// StatusLine: cached from refreshMeta (transcript.Tracker.StatusLine).
//
// Single-threaded: the driverActor wrapper guarantees View runs on the
// same goroutine as Tick / HandleEvent, so the cached fields are stable
// for the duration of the call.
func (d *claudeDriver) View() SessionView {
	tags := []Tag{CommandTag(d.Name())}
	if t := BranchTag(d.branchTag); t.Text != "" {
		tags = append(tags, t)
	}

	var logTabs []LogTab
	if d.transcriptPath != "" {
		logTabs = append(logTabs, LogTab{
			Label: "TRANSCRIPT",
			Path:  d.transcriptPath,
			Kind:  TabKindTranscript,
		})
	}
	if d.eventLogDir != "" && d.sessionID != "" {
		logTabs = append(logTabs, LogTab{
			Label: "EVENTS",
			Path:  filepath.Join(d.eventLogDir, d.sessionID+".log"),
			Kind:  TabKindText,
		})
	}

	return SessionView{
		Card: CardView{
			Title:      d.title,
			Subtitle:   firstNonEmpty(d.summary, d.lastPrompt),
			Tags:       tags,
			Indicators: d.indicators(),
		},
		LogTabs:    logTabs,
		InfoExtras: d.infoExtras(),
		StatusLine: d.statusLine,
	}
}

// indicators formats the chip strings for the card.
func (d *claudeDriver) indicators() []string {
	var out []string
	if d.currentTool != "" {
		out = append(out, "▸ "+d.currentTool)
	}
	subs := 0
	for _, n := range d.subagentCounts {
		subs += n
	}
	if subs > 0 {
		out = append(out, fmt.Sprintf("%d subs", subs))
	}
	return out
}

// infoExtras builds the driver-specific INFO lines. Only fields that are
// NOT already visible via Card.Tags or Card.Indicators are included
// here — the TUI re-renders Tags and Indicators as bullet sections in
// the INFO tab, so listing them as InfoExtras would duplicate the same
// value.
func (d *claudeDriver) infoExtras() []InfoLine {
	var lines []InfoLine
	add := func(label, value string) {
		if value != "" {
			lines = append(lines, InfoLine{Label: label, Value: value})
		}
	}
	add("Title", d.title)
	add("Summary", d.summary)
	add("Last Prompt", d.lastPrompt)
	add("Working Dir", d.workingDir)
	add("Transcript", d.transcriptPath)
	return lines
}

// firstNonEmpty returns the first non-empty string from its arguments,
// or "" when none qualify. Used by View() to fall back through ordered
// candidates (e.g. summary → lastPrompt) without nested ternaries.
func firstNonEmpty(candidates ...string) string {
	for _, s := range candidates {
		if s != "" {
			return s
		}
	}
	return ""
}

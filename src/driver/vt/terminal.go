package vt

import (
	"bytes"
	"strconv"
	"strings"

	xvt "github.com/charmbracelet/x/vt"
)

// Terminal wraps charmbracelet/x/vt Emulator and exposes a minimal
// surface for agent-roost's driver state detection.
//
// All methods must be called from the same goroutine (the worker-pool
// job goroutine). No internal locking is added because the driver
// reducer and its worker each own a single logical execution thread.
type Terminal struct {
	em                  *xvt.Emulator
	pending             []OscNotification
	pendingPromptEvents []PromptEvent
	prevStable          string
}

// New creates a Terminal sized cols×rows. Defaults to 80×24 for zero values.
func New(cols, rows int) *Terminal {
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	t := &Terminal{em: xvt.NewEmulator(cols, rows)}
	t.registerOscHandlers()
	return t
}

// Resize adjusts the emulator viewport to cols×rows.
func (t *Terminal) Resize(cols, rows int) {
	t.em.Resize(cols, rows)
}

// Feed writes raw ANSI bytes (e.g. from tmux capture-pane -e) into the
// emulator.
func (t *Terminal) Feed(data []byte) error {
	_, err := t.em.Write(data)
	return err
}

// Snapshot captures the current screen state and flushes pending OSC
// notifications and prompt events. It updates internal prevStable for
// DirtyCount tracking.
func (t *Terminal) Snapshot() Snapshot {
	notifs := t.pending
	t.pending = nil
	promptEvents := t.pendingPromptEvents
	t.pendingPromptEvents = nil
	snap := computeSnapshot(t.em, t.prevStable, notifs, promptEvents)
	t.prevStable = snap.Stable
	return snap
}

// Reset clears the emulator buffer and resets DirtyCount tracking so the
// next Snapshot is treated as a fresh baseline.
func (t *Terminal) Reset() {
	cols, rows := t.em.Width(), t.em.Height()
	t.em = xvt.NewEmulator(cols, rows)
	t.registerOscHandlers()
	t.prevStable = ""
	t.pending = nil
	t.pendingPromptEvents = nil
}

func (t *Terminal) registerOscHandlers() {
	for _, cmd := range []int{9, 99, 777} {
		t.em.RegisterOscHandler(cmd, func(data []byte) bool {
			// data is the full OSC payload including the leading "<cmd>;"
			// prefix (e.g. "9;Hello from agent").  Strip the numeric prefix.
			payload := string(data)
			if i := bytes.IndexByte(data, ';'); i >= 0 {
				payload = string(data[i+1:])
			}
			t.pending = append(t.pending, OscNotification{Cmd: cmd, Payload: payload})
			return false
		})
	}

	// OSC 133: semantic shell prompts (FinalTerm / shell integration protocol).
	// Payload format after stripping "133;": A | B | C | D[;<exit-code>]
	t.em.RegisterOscHandler(133, func(data []byte) bool {
		// Strip "133;" prefix.
		payload := string(data)
		if i := bytes.IndexByte(data, ';'); i >= 0 {
			payload = string(data[i+1:])
		}
		parts := strings.SplitN(payload, ";", 2)
		if len(parts) == 0 {
			return false
		}
		var ev PromptEvent
		switch parts[0] {
		case "A":
			ev.Phase = PromptPhaseStart
		case "B":
			ev.Phase = PromptPhaseInput
		case "C":
			ev.Phase = PromptPhaseCommand
		case "D":
			ev.Phase = PromptPhaseComplete
			if len(parts) == 2 {
				if code, err := strconv.Atoi(parts[1]); err == nil {
					ev.ExitCode = &code
				}
			}
		default:
			return false // unknown phase — drop silently
		}
		t.pendingPromptEvents = append(t.pendingPromptEvents, ev)
		return false
	})
}

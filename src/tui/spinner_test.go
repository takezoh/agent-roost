package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
)

func TestRunningSpinnerPalette(t *testing.T) {
	ApplyTheme("default")
	if len(runningPalette) != 24 {
		t.Fatalf("runningPalette length = %d, want 24", len(runningPalette))
	}
}

func TestRunningSpinnerGlyphCycles(t *testing.T) {
	ApplyTheme("default")
	frames := spinner.MiniDot.Frames
	for i, want := range frames {
		animFrame = uint64(i)
		got := runningSpinnerGlyph()
		// Strip ANSI escape sequences to compare only the glyph character.
		plain := stripANSI(got)
		if !strings.Contains(plain, want) {
			t.Errorf("frame %d: got %q, want glyph %q", i, plain, want)
		}
	}
}

func TestRunningSpinnerGlyphWraps(t *testing.T) {
	ApplyTheme("default")
	frames := spinner.MiniDot.Frames
	n := len(frames)

	animFrame = 0
	g0 := stripANSI(runningSpinnerGlyph())

	animFrame = uint64(n) // should wrap back to frame 0
	gN := stripANSI(runningSpinnerGlyph())

	if g0 != gN {
		t.Errorf("wrap failed: frame 0 = %q, frame %d = %q", g0, n, gN)
	}
}

func TestRunningSpinnerPaletteRotates(t *testing.T) {
	ApplyTheme("default")
	// Consecutive frames should use different palette colors.
	// We verify by checking that at least two consecutive entries in the
	// palette differ — the Blend1D interpolation guarantees non-constant output.
	same := true
	for i := 1; i < len(runningPalette); i++ {
		if runningPalette[i] != runningPalette[i-1] {
			same = false
			break
		}
	}
	if same {
		t.Error("runningPalette is constant; expected gradient variation")
	}
}

// stripANSI removes ANSI CSI escape sequences from s so that glyph content
// can be compared independently of terminal color codes.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEsc = true
		case inEsc && r == 'm':
			inEsc = false
		case inEsc:
			// still inside escape sequence
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

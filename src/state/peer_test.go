package state

import (
	"strings"
	"testing"
)

func TestFormatPeerMessage(t *testing.T) {
	from := FrameID("abcdef1234567890")
	t.Run("with summary", func(t *testing.T) {
		got := formatPeerMessage(from, "doing code review", "hello there")
		want := "[peer-msg from=abcdef12 (doing code review)]\nhello there"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("without summary", func(t *testing.T) {
		got := formatPeerMessage(from, "", "hello there")
		want := "[peer-msg from=abcdef12]\nhello there"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("no leading whitespace", func(t *testing.T) {
		got := formatPeerMessage(from, "", "msg")
		if strings.HasPrefix(got, "\n") || strings.HasPrefix(got, " ") {
			t.Errorf("unexpected leading whitespace: %q", got)
		}
	})

	t.Run("no trailing whitespace", func(t *testing.T) {
		got := formatPeerMessage(from, "", "msg")
		if strings.HasSuffix(got, "\n") || strings.HasSuffix(got, " ") {
			t.Errorf("unexpected trailing whitespace: %q", got)
		}
	})
}

func TestPeerShortID(t *testing.T) {
	t.Run("long id truncated to 8", func(t *testing.T) {
		id := FrameID("abcdef1234567890")
		got := peerShortID(id)
		if len(got) != 8 {
			t.Errorf("len = %d, want 8", len(got))
		}
		if got != "abcdef12" {
			t.Errorf("got %q, want abcdef12", got)
		}
	})

	t.Run("short id returned as-is", func(t *testing.T) {
		id := FrameID("abc")
		got := peerShortID(id)
		if got != "abc" {
			t.Errorf("got %q, want abc", got)
		}
	})

	t.Run("exactly 8 chars", func(t *testing.T) {
		id := FrameID("12345678")
		got := peerShortID(id)
		if got != "12345678" {
			t.Errorf("got %q, want 12345678", got)
		}
	})
}

func TestAllocMsgID(t *testing.T) {
	id := allocMsgID()
	if len(id) != 8 {
		t.Errorf("len = %d, want 8", len(id))
	}

	// IDs should be random (two consecutive calls produce different results).
	id2 := allocMsgID()
	if id == id2 {
		t.Error("two consecutive allocMsgID calls returned the same value (very unlikely if random)")
	}

	// IDs should be hex characters only.
	for _, c := range id {
		if !isHexChar(c) {
			t.Errorf("non-hex char %q in id %q", c, id)
		}
	}
}

func isHexChar(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
}

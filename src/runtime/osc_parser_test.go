package runtime

import (
	"testing"
)

func TestOscParserFeed_OSC9(t *testing.T) {
	p := &oscParser{}
	input := []byte("\x1b]9;Hello from agent\x1b\\")
	seqs := p.feed(input)
	if len(seqs) != 1 {
		t.Fatalf("expected 1 seq, got %d", len(seqs))
	}
	if seqs[0].cmd != 9 || seqs[0].payload != "Hello from agent" {
		t.Errorf("unexpected seq: %+v", seqs[0])
	}
}

func TestOscParserFeed_OSC9_BEL(t *testing.T) {
	p := &oscParser{}
	input := []byte("\x1b]9;BEL test\x07")
	seqs := p.feed(input)
	if len(seqs) != 1 || seqs[0].cmd != 9 || seqs[0].payload != "BEL test" {
		t.Errorf("unexpected seqs: %+v", seqs)
	}
}

func TestOscParserFeed_SkipsNonNotification(t *testing.T) {
	p := &oscParser{}
	// OSC 8 (hyperlink) should be ignored
	input := []byte("\x1b]8;;https://example.com\x1b\\linktext\x1b]8;;\x1b\\")
	seqs := p.feed(input)
	if len(seqs) != 0 {
		t.Errorf("expected no seqs, got %d", len(seqs))
	}
}

func TestOscParserFeed_SplitAcrossChunks(t *testing.T) {
	p := &oscParser{}
	seqs := p.feed([]byte("\x1b]9;Hel"))
	if len(seqs) != 0 {
		t.Error("should not emit before terminator")
	}
	seqs = p.feed([]byte("lo\x07"))
	if len(seqs) != 1 || seqs[0].payload != "Hello" {
		t.Errorf("unexpected seqs: %+v", seqs)
	}
}

func TestOscParserFeed_OSC777(t *testing.T) {
	p := &oscParser{}
	seqs := p.feed([]byte("\x1b]777;notify;My Title;My Body\x07"))
	if len(seqs) != 1 || seqs[0].cmd != 777 {
		t.Fatalf("unexpected seqs: %+v", seqs)
	}
}

func TestParseOscPayload_OSC9(t *testing.T) {
	title, body := parseOscPayload(9, "  hello  ")
	if title != "hello" || body != "" {
		t.Errorf("got title=%q body=%q", title, body)
	}
}

func TestParseOscPayload_OSC777(t *testing.T) {
	title, body := parseOscPayload(777, "notify;My Title;My Body")
	if title != "My Title" || body != "My Body" {
		t.Errorf("got title=%q body=%q", title, body)
	}
}

func TestParseOscPayload_OSC99(t *testing.T) {
	title, body := parseOscPayload(99, "i=1:d=Alert:p=Something happened")
	if title != "Alert" || body != "Something happened" {
		t.Errorf("got title=%q body=%q", title, body)
	}
}

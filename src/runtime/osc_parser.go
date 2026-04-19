package runtime

import "strings"

// oscParser extracts OSC sequences from a raw terminal byte stream.
// It is a lightweight state machine that does not perform full VT emulation;
// it only captures OSC 0 / 9 / 99 / 777 sequences needed for EvPaneOsc.
// State is carried across successive feed calls.
type oscParser struct {
	buf   []byte
	inOsc bool
}

type oscSeq struct {
	cmd     int
	payload string
}

// feed processes data and returns any complete OSC notification sequences found.
func (p *oscParser) feed(data []byte) []oscSeq {
	var out []oscSeq
	for _, b := range data {
		switch {
		case !p.inOsc && b == 0x1B:
			p.buf = append(p.buf[:0], b)

		case len(p.buf) == 1 && p.buf[0] == 0x1B && b == 0x5D: // ESC ]
			p.buf = p.buf[:0]
			p.inOsc = true

		case p.inOsc:
			isST := b == 0x5C && len(p.buf) > 0 && p.buf[len(p.buf)-1] == 0x1B
			if b == 0x07 || isST {
				raw := string(p.buf)
				if isST {
					raw = raw[:len(raw)-1] // strip the leading ESC of ST
				}
				if seq, ok := splitOscSeq(raw); ok {
					out = append(out, seq)
				}
				p.buf = p.buf[:0]
				p.inOsc = false
			} else if len(p.buf) >= 4096 {
				// Guard against unterminated OSC sequences consuming unbounded memory.
				p.buf = p.buf[:0]
				p.inOsc = false
			} else {
				p.buf = append(p.buf, b)
			}

		default:
			p.buf = p.buf[:0]
		}
	}
	return out
}

// splitOscSeq splits "cmd;payload" and filters to notification commands only.
func splitOscSeq(s string) (oscSeq, bool) {
	idx := strings.IndexByte(s, ';')
	if idx < 0 {
		return oscSeq{}, false
	}
	cmdStr := s[:idx]
	cmd := 0
	for _, c := range cmdStr {
		if c < '0' || c > '9' {
			return oscSeq{}, false
		}
		cmd = cmd*10 + int(c-'0')
	}
	if cmd != 0 && cmd != 9 && cmd != 99 && cmd != 777 {
		return oscSeq{}, false
	}
	return oscSeq{cmd: cmd, payload: s[idx+1:]}, true
}

// parseOscPayload extracts (title, body) from an OSC notification payload.
func parseOscPayload(cmd int, payload string) (title, body string) {
	switch cmd {
	case 0:
		return strings.TrimSpace(payload), ""
	case 9:
		return strings.TrimSpace(payload), ""
	case 777:
		parts := strings.SplitN(payload, ";", 3)
		if len(parts) >= 3 {
			return parts[1], parts[2]
		}
		if len(parts) == 2 {
			return parts[1], ""
		}
	case 99:
		for _, part := range strings.Split(payload, ":") {
			k, v, ok := strings.Cut(part, "=")
			if !ok {
				continue
			}
			switch k {
			case "d":
				title = v
			case "p":
				body = v
			}
		}
		if title == "" && body == "" {
			body = payload
		}
	}
	return title, body
}

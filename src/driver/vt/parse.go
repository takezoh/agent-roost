package vt

import "strings"

// ParseOscNotification extracts title and body from an OSC notification payload.
// OSC 9 (iTerm2): payload is the title text.
// OSC 777 (urxvt): payload is "notify;<title>;<body>".
// OSC 99 (Kitty): colon-separated key=value pairs; d= is title, p= is body.
func ParseOscNotification(n OscNotification) (title, body string) {
	switch n.Cmd {
	case 9:
		return strings.TrimSpace(n.Payload), ""
	case 777:
		parts := strings.SplitN(n.Payload, ";", 3)
		if len(parts) >= 3 {
			return parts[1], parts[2]
		}
		if len(parts) == 2 {
			return parts[1], ""
		}
	case 99:
		title, body = parseKittyPayload(n.Payload)
		if title == "" && body == "" {
			body = n.Payload
		}
	}
	return title, body
}

func parseKittyPayload(payload string) (title, body string) {
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
	return title, body
}

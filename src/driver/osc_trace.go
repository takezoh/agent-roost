package driver

import "os"

// oscTraceEnabled reports whether ROOST_OSC_TRACE=1 is set.
// When active, feedAndSnapshot logs OSC byte marker counts at debug
// level so operators can verify that OSC sequences reach the VT
// capture path (distinct from the main pipe-pane tap).
func oscTraceEnabled() bool { return os.Getenv("ROOST_OSC_TRACE") == "1" }

// countOscMarkers counts ESC ] (0x1b 0x5d) byte pairs in b.
func countOscMarkers(b []byte) (n int) {
	for i := 0; i < len(b)-1; i++ {
		if b[i] == 0x1b && b[i+1] == 0x5d {
			n++
		}
	}
	return n
}

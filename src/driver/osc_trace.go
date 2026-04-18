package driver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// oscTraceEnabled reports whether ROOST_OSC_TRACE=1 is set.
func oscTraceEnabled() bool { return os.Getenv("ROOST_OSC_TRACE") == "1" }

// oscTracePipeEnabled reports whether ROOST_OSC_TRACE_PIPE=1 is set.
func oscTracePipeEnabled() bool { return os.Getenv("ROOST_OSC_TRACE_PIPE") == "1" }

// countOscMarkers counts ESC ] (0x1b 0x5d) byte pairs in b.
func countOscMarkers(b []byte) (n int) {
	for i := 0; i < len(b)-1; i++ {
		if b[i] == 0x1b && b[i+1] == 0x5d {
			n++
		}
	}
	return n
}

var (
	oscPipeMu     sync.Mutex
	oscPipePaneFn func(pane, cmd string) error
	oscPipeDir    string
)

// SetOscPipeTracer registers the tmux pipe-pane function and the directory
// where per-pane raw trace files will be written.  Call once from the
// coordinator when ROOST_OSC_TRACE_PIPE=1 before any panes are observed.
func SetOscPipeTracer(fn func(pane, cmd string) error, stateDir string) {
	oscPipeMu.Lock()
	defer oscPipeMu.Unlock()
	oscPipePaneFn = fn
	oscPipeDir = filepath.Join(stateDir, "osc-trace")
	_ = os.MkdirAll(oscPipeDir, 0o755)
}

// startOscPipe starts a pipe-pane for pane if ROOST_OSC_TRACE_PIPE is active.
func startOscPipe(pane string) {
	if !oscTracePipeEnabled() {
		return
	}
	oscPipeMu.Lock()
	fn := oscPipePaneFn
	dir := oscPipeDir
	oscPipeMu.Unlock()
	if fn == nil || dir == "" {
		return
	}
	safe := strings.NewReplacer("%", "_", ":", "_").Replace(pane)
	path := filepath.Join(dir, safe+".raw")
	cmd := fmt.Sprintf("cat >> %s", path)
	if err := fn(pane, cmd); err != nil {
		// best-effort; log at startup only
		_ = err
	}
}

// stopOscPipe stops the pipe-pane for pane.
func stopOscPipe(pane string) {
	if !oscTracePipeEnabled() {
		return
	}
	oscPipeMu.Lock()
	fn := oscPipePaneFn
	oscPipeMu.Unlock()
	if fn == nil {
		return
	}
	_ = fn(pane, "")
}

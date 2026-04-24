package runtime

import (
	"log/slog"
	"sync"

	"github.com/takezoh/agent-roost/state"
)

// storeFrameCleanup registers a Cleanup callback for a frame. Called from
// goroutines (spawnTmuxWindowAsync) so the map access is mutex-guarded.
func (r *Runtime) storeFrameCleanup(frameID state.FrameID, fn func() error) {
	r.frameCleanupsMu.Lock()
	r.frameCleanups[frameID] = fn
	r.frameCleanupsMu.Unlock()
}

// invokeFrameCleanup retrieves the registered cleanup for the frame, removes
// it from the map, and runs it in a goroutine so the event loop is not blocked.
func (r *Runtime) invokeFrameCleanup(frameID state.FrameID) {
	r.frameCleanupsMu.Lock()
	fn := r.frameCleanups[frameID]
	delete(r.frameCleanups, frameID)
	r.frameCleanupsMu.Unlock()
	if fn == nil {
		return
	}
	go func() {
		if err := fn(); err != nil {
			slog.Warn("runtime: frame cleanup failed", "frame", frameID, "err", err)
		}
	}()
}

// drainFrameCleanups invokes all pending frame cleanups concurrently and
// waits for them to finish. Called at daemon shutdown before the launcher
// itself is shut down.
func (r *Runtime) drainFrameCleanups() {
	r.frameCleanupsMu.Lock()
	fns := r.frameCleanups
	r.frameCleanups = map[state.FrameID]func() error{}
	r.frameCleanupsMu.Unlock()
	var wg sync.WaitGroup
	for frameID, fn := range fns {
		wg.Add(1)
		go func(frameID state.FrameID, fn func() error) {
			defer wg.Done()
			if err := fn(); err != nil {
				slog.Warn("runtime: frame cleanup (drain) failed", "frame", frameID, "err", err)
			}
		}(frameID, fn)
	}
	wg.Wait()
}

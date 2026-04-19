package runtime

import (
	"context"
	"log/slog"
	"time"

	"github.com/takezoh/agent-roost/state"
)

// tapActivityDebounce is the minimum interval between consecutive EvPaneActivity
// events emitted by a tap reader. Prevents flooding the event loop when the
// pane is producing high-volume output.
const tapActivityDebounce = 100 * time.Millisecond

// tapEntry holds the cancel function and pane identifier for one running tap.
type tapEntry struct {
	cancel context.CancelFunc
	pane   string
}

// tapManager starts and stops PaneTap reader goroutines per frame.
// All methods must be called from the event loop goroutine.
type tapManager struct {
	tap     PaneTap
	ctx     context.Context
	cancels map[state.FrameID]tapEntry
}

func newTapManager(ctx context.Context, tap PaneTap) *tapManager {
	return &tapManager{
		tap:     tap,
		ctx:     ctx,
		cancels: map[state.FrameID]tapEntry{},
	}
}

// start begins a tap for the given frame/pane pair. If a tap already exists
// for frameID it is stopped first.
func (m *tapManager) start(frameID state.FrameID, pane string, enqueue func(state.Event)) {
	if m.tap == nil {
		return
	}
	m.stop(frameID)

	tapCtx, cancel := context.WithCancel(m.ctx)
	ch, err := m.tap.Start(tapCtx, pane)
	if err != nil {
		slog.Warn("panetap: start failed", "frame", frameID, "pane", pane, "err", err)
		cancel()
		return
	}
	slog.Info("panetap: started", "frame", frameID, "pane", pane)
	m.cancels[frameID] = tapEntry{cancel: cancel, pane: pane}
	go readTap(tapCtx, frameID, pane, ch, enqueue)
}

// stop cancels the reader goroutine and stops the underlying pipe-pane process.
func (m *tapManager) stop(frameID state.FrameID) {
	entry, ok := m.cancels[frameID]
	if !ok {
		return
	}
	entry.cancel()
	if err := m.tap.Stop(entry.pane); err != nil {
		slog.Debug("panetap: stop failed", "frame", frameID, "pane", entry.pane, "err", err)
	}
	delete(m.cancels, frameID)
}

// stopAll cancels all running taps. Called on daemon shutdown.
func (m *tapManager) stopAll() {
	for id := range m.cancels {
		m.stop(id)
	}
}

// readTap parses raw bytes from ch and emits two event types:
//   - EvPaneActivity: debounced (100ms), emitted on any byte receipt so the
//     event loop can trigger a capture-pane without waiting for the next tick.
//   - EvPaneOsc: emitted per OSC sequence for notification routing.
//
// Runs in its own goroutine; exits when ch is closed or ctx is cancelled.
func readTap(ctx context.Context, frameID state.FrameID, pane string, ch <-chan []byte, enqueue func(state.Event)) {
	parser := &oscParser{}
	var lastActivity time.Time
	for {
		select {
		case data, ok := <-ch:
			if !ok {
				return
			}
			now := time.Now()
			if now.Sub(lastActivity) >= tapActivityDebounce {
				lastActivity = now
				enqueue(state.EvPaneActivity{
					FrameID:    frameID,
					PaneTarget: pane,
					Now:        now,
				})
			}
			for _, seq := range parser.feed(data) {
				title, body := parseOscPayload(seq.cmd, seq.payload)
				if title == "" && body == "" {
					continue
				}
				enqueue(state.EvPaneOsc{
					FrameID: frameID,
					Cmd:     seq.cmd,
					Title:   title,
					Body:    body,
					Now:     now,
				})
			}
		case <-ctx.Done():
			return
		}
	}
}

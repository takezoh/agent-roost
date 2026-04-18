package runtime

import (
	"context"
	"log/slog"

	"github.com/takezoh/agent-roost/state"
)

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
	m.cancels[frameID] = tapEntry{cancel: cancel, pane: pane}
	go readTap(tapCtx, frameID, ch, enqueue)
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

// readTap parses raw bytes from ch for OSC notification sequences and emits
// EvPaneOsc events. Runs in its own goroutine; exits when ch is closed or ctx
// is cancelled.
func readTap(ctx context.Context, frameID state.FrameID, ch <-chan []byte, enqueue func(state.Event)) {
	parser := &oscParser{}
	for {
		select {
		case data, ok := <-ch:
			if !ok {
				return
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
				})
			}
		case <-ctx.Done():
			return
		}
	}
}

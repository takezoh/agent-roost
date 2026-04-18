package runtime

import "context"

// PaneTap is a source of raw terminal byte streams from a tmux pane.
// The event loop starts one tap per frame when the pane is registered
// and stops it when the pane is unregistered. The reader goroutine feeds
// bytes into a vt.Terminal and emits EvPaneOsc events for any OSC
// notifications detected.
//
// tmux pipe-pane is the current implementation. Future implementations
// may read from a PTY or libghostty session without changing this interface.
type PaneTap interface {
	// Start begins delivering raw bytes for pane into the returned channel.
	// The channel is closed when the tap is stopped or ctx is cancelled.
	Start(ctx context.Context, pane string) (<-chan []byte, error)
	// Stop ends delivery for pane and releases all resources.
	Stop(pane string) error
}

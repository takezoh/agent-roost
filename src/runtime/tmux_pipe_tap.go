package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TmuxPipePaneTap implements PaneTap using tmux pipe-pane. Each started pane
// pipes its raw output to a temporary file via "cat >>". A reader goroutine
// tails the file and delivers chunks to the channel.
type TmuxPipePaneTap struct {
	pipeFn func(pane, cmd string) error
	dir    string
}

// NewTmuxPipePaneTap creates a tap backed by the given pipe-pane function.
// dir is the directory where per-pane buffer files are written.
func NewTmuxPipePaneTap(pipeFn func(pane, cmd string) error, dir string) *TmuxPipePaneTap {
	return &TmuxPipePaneTap{pipeFn: pipeFn, dir: dir}
}

func (t *TmuxPipePaneTap) Start(ctx context.Context, pane string) (<-chan []byte, error) {
	path := filepath.Join(t.dir, safePaneName(pane)+".buf")
	os.Remove(path) //nolint:errcheck

	if err := t.pipeFn(pane, "cat >> "+path); err != nil {
		return nil, fmt.Errorf("pipe-pane start: %w", err)
	}

	// Create the file immediately so the open below always succeeds even
	// if cat has not written the first byte yet.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0600)
	if err != nil {
		_ = t.pipeFn(pane, "") // stop the pipe
		return nil, fmt.Errorf("open buf: %w", err)
	}

	ch := make(chan []byte, 64)
	go tailFile(ctx, f, path, ch)
	return ch, nil
}

func (t *TmuxPipePaneTap) Stop(pane string) error {
	err := t.pipeFn(pane, "")
	path := filepath.Join(t.dir, safePaneName(pane)+".buf")
	os.Remove(path) //nolint:errcheck
	return err
}

// tailFile reads new data from f in a polling loop until ctx is cancelled.
// It closes ch and removes path on exit.
func tailFile(ctx context.Context, f *os.File, path string, ch chan<- []byte) {
	defer close(ch)
	defer f.Close()
	defer os.Remove(path) //nolint:errcheck

	buf := make([]byte, 4096)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		n, err := f.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			select {
			case ch <- data:
			case <-ctx.Done():
				return
			}
			continue // read more immediately before waiting for tick
		}
		if err != nil && !errors.Is(err, io.EOF) {
			slog.Debug("panetap: read error", "path", path, "err", err)
			return
		}
		// No new data — wait for next tick or ctx cancel.
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func safePaneName(pane string) string {
	return strings.NewReplacer("%", "pct", ":", "_").Replace(pane)
}

package runtime

import (
	"log/slog"
	"sync"

	"github.com/fsnotify/fsnotify"

	"github.com/takezoh/agent-roost/state"
)

// RealFSWatcher is the production FSWatcher backed by fsnotify. It
// watches per-frame transcript files and emits FSEvent on Events()
// when they change. The watcher exposes a single events channel; the
// runtime forwards each event into the event loop on the next select
// iteration.
//
// fsnotify watches by path, so we maintain a path → frameID map to
// translate raw fsnotify events into runtime FSEvents.
type RealFSWatcher struct {
	w *fsnotify.Watcher

	mu          sync.Mutex
	pathToFrame map[string]state.FrameID
	frameToPath map[state.FrameID]string

	out chan FSEvent

	stop    chan struct{}
	stopped chan struct{}
}

// NewRealFSWatcher constructs and starts the watcher's relay
// goroutine. Close shuts both the underlying fsnotify watcher and the
// relay.
func NewRealFSWatcher() (*RealFSWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	r := &RealFSWatcher{
		w:           w,
		pathToFrame: map[string]state.FrameID{},
		frameToPath: map[state.FrameID]string{},
		out:         make(chan FSEvent, 64),
		stop:        make(chan struct{}),
		stopped:     make(chan struct{}),
	}
	go r.relay()
	return r, nil
}

// Watch registers a transcript file with the underlying watcher and
// records the frameID mapping. If the frame was watching a
// different path before, the old watch is removed.
func (r *RealFSWatcher) Watch(frameID state.FrameID, path string) error {
	if path == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if old, ok := r.frameToPath[frameID]; ok && old != path {
		r.removeLocked(old)
	}
	if _, ok := r.pathToFrame[path]; !ok {
		if err := r.w.Add(path); err != nil {
			return err
		}
	}
	r.pathToFrame[path] = frameID
	r.frameToPath[frameID] = path
	return nil
}

// Unwatch removes a frame's watch. Idempotent.
func (r *RealFSWatcher) Unwatch(frameID state.FrameID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if path, ok := r.frameToPath[frameID]; ok {
		r.removeLocked(path)
		delete(r.frameToPath, frameID)
	}
	return nil
}

func (r *RealFSWatcher) removeLocked(path string) {
	if err := r.w.Remove(path); err != nil {
		slog.Debug("fswatcher: remove failed", "path", path, "err", err)
	}
	delete(r.pathToFrame, path)
}

// Events returns the channel runtime selects on inside the event loop.
func (r *RealFSWatcher) Events() <-chan FSEvent { return r.out }

// Close stops the relay goroutine and shuts down fsnotify.
func (r *RealFSWatcher) Close() error {
	close(r.stop)
	<-r.stopped
	return r.w.Close()
}

// relay reads from the underlying fsnotify channel, looks up the
// frame id from the path map, and forwards a typed FSEvent to the
// runtime. Errors from fsnotify are logged at debug level.
func (r *RealFSWatcher) relay() {
	defer close(r.stopped)
	for {
		select {
		case <-r.stop:
			return
		case ev, ok := <-r.w.Events:
			if !ok {
				return
			}
			// Only Write / Create events meaningfully change content.
			if !ev.Has(fsnotify.Write) && !ev.Has(fsnotify.Create) {
				continue
			}
			r.mu.Lock()
			frameID, found := r.pathToFrame[ev.Name]
			r.mu.Unlock()
			if !found {
				continue
			}
			select {
			case r.out <- FSEvent{FrameID: frameID, Path: ev.Name}:
			default:
				slog.Warn("fswatcher: out channel full, dropping",
					"path", ev.Name)
			}
		case err, ok := <-r.w.Errors:
			if !ok {
				return
			}
			slog.Debug("fswatcher: error", "err", err)
		}
	}
}

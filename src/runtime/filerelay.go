package runtime

import (
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/state"
)

// FileRelay watches log and session files via fsnotify, reads new
// bytes when they change, and broadcasts them as EvtLogLine or
// EvtSessionFileLine events to IPC subscribers. This replaces the
// TUI's 200ms polling loop — the TUI just receives and renders.
//
// FileRelay owns its own fsnotify watcher (separate from the
// runtime's transcript watcher, which feeds the driver state
// machine). Each file is tracked independently with an offset and
// a "dirty" flag. A background goroutine runs a 100ms sweep that
// reads all dirty files and broadcasts new content in one batch.
type FileRelay struct {
	mu      sync.Mutex
	watcher *fsnotify.Watcher
	files   map[string]*relayFile
	rt      *Runtime

	stop    chan struct{}
	wg      sync.WaitGroup
}

type relayFile struct {
	path      string
	sessionID state.SessionID // empty for app log
	kind      string          // "log" or "transcript"
	offset    int64
	dirty     bool
}

const relaySweepInterval = 100 * time.Millisecond

// NewFileRelay creates and starts a file relay for the given runtime.
func NewFileRelay(rt *Runtime) (*FileRelay, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	fr := &FileRelay{
		watcher: w,
		files:   map[string]*relayFile{},
		rt:      rt,
		stop:    make(chan struct{}),
	}
	fr.wg.Add(2)
	go fr.watchLoop()
	go fr.sweepLoop()
	return fr, nil
}

// WatchLog registers the app log file for push relay.
func (fr *FileRelay) WatchLog(path string) {
	fr.add(path, "", "log")
}

// WatchFile registers a session file (transcript, event-log, etc.) for push relay.
func (fr *FileRelay) WatchFile(sessionID state.SessionID, path string, kind string) {
	fr.add(path, sessionID, kind)
}

// UnwatchFile removes all files associated with a session from the relay.
func (fr *FileRelay) UnwatchFile(sessionID state.SessionID) {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	for path, f := range fr.files {
		if f.sessionID == sessionID {
			fr.watcher.Remove(path)
			delete(fr.files, path)
		}
	}
}

// Unwatch removes a file from the relay by path.
func (fr *FileRelay) Unwatch(path string) {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	if _, ok := fr.files[path]; ok {
		fr.watcher.Remove(path)
		delete(fr.files, path)
	}
}

// Close shuts down the relay, waiting for both goroutines to exit.
func (fr *FileRelay) Close() {
	close(fr.stop)
	fr.wg.Wait()
	fr.watcher.Close()
}

func (fr *FileRelay) add(path string, sessionID state.SessionID, kind string) {
	if path == "" {
		return
	}
	fr.mu.Lock()
	defer fr.mu.Unlock()
	if _, ok := fr.files[path]; ok {
		return
	}
	// Seek to end — we don't backfill from the relay. The TUI does
	// its own backfill on tab switch via direct file reads.
	var offset int64
	if info, err := os.Stat(path); err == nil {
		offset = info.Size()
	}
	fr.files[path] = &relayFile{
		path:      path,
		sessionID: sessionID,
		kind:      kind,
		offset:    offset,
	}
	if err := fr.watcher.Add(path); err != nil {
		slog.Debug("filerelay: watch failed", "path", path, "err", err)
	}
}

// watchLoop listens for fsnotify events and marks files as dirty.
func (fr *FileRelay) watchLoop() {
	defer fr.wg.Done()
	for {
		select {
		case <-fr.stop:
			return
		case ev, ok := <-fr.watcher.Events:
			if !ok {
				return
			}
			if !ev.Has(fsnotify.Write) && !ev.Has(fsnotify.Create) {
				continue
			}
			fr.mu.Lock()
			if f, ok := fr.files[ev.Name]; ok {
				f.dirty = true
			}
			fr.mu.Unlock()
		case err, ok := <-fr.watcher.Errors:
			if !ok {
				return
			}
			slog.Debug("filerelay: fsnotify error", "err", err)
		}
	}
}

// sweepLoop runs every relaySweepInterval and reads all dirty files.
func (fr *FileRelay) sweepLoop() {
	defer fr.wg.Done()
	ticker := time.NewTicker(relaySweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-fr.stop:
			return
		case <-ticker.C:
			fr.sweep()
		}
	}
}

func (fr *FileRelay) sweep() {
	fr.mu.Lock()
	// Snapshot dirty files under lock, then release for I/O.
	var dirty []*relayFile
	for _, f := range fr.files {
		if f.dirty {
			f.dirty = false
			dirty = append(dirty, f)
		}
	}
	fr.mu.Unlock()

	for _, f := range dirty {
		content, newOffset := readFrom(f.path, f.offset)
		if content == "" {
			continue
		}
		fr.mu.Lock()
		f.offset = newOffset
		fr.mu.Unlock()

		fr.broadcast(f, content)
	}
}

func readFrom(path string, offset int64) (string, int64) {
	file, err := os.Open(path)
	if err != nil {
		return "", offset
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", offset
	}
	// Truncation detection
	if info.Size() < offset {
		offset = 0
	}
	if info.Size() == offset {
		return "", offset
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return "", offset
	}
	data, err := io.ReadAll(io.LimitReader(file, info.Size()-offset))
	if err != nil {
		return "", offset
	}
	return string(data), offset + int64(len(data))
}

func (fr *FileRelay) broadcast(f *relayFile, content string) {
	var event proto.ServerEvent
	if f.sessionID == "" {
		event = proto.EvtLogLine{Path: f.path, Line: content}
	} else {
		event = proto.EvtSessionFileLine{
			SessionID: string(f.sessionID),
			Kind:      f.kind,
			Line:      content,
		}
	}
	wire, err := proto.EncodeEvent(event)
	if err != nil {
		return
	}
	fr.rt.broadcastWire(wire, event.EventName())
}

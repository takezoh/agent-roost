package tui

import (
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// readNewLines tails the file behind tab from its previously recorded
// offset, returning whatever lines have been appended since the last read.
// If the file has shrunk (rotated or truncated), the reader is reset and
// an empty string is returned. Partial trailing lines (no final '\n') are
// stashed in tab.buf and not returned until the next read completes them.
func readNewLines(tab *tabState) (string, error) {
	if err := openTabFile(tab); err != nil {
		return "", err
	}
	info, err := tab.file.Stat()
	if err != nil {
		tab.file.Close()
		tab.file = nil
		return "", err
	}
	if info.Size() < tab.offset {
		tab.file.Close()
		tab.file = nil
		tab.offset = 0
		tab.buf = ""
		return "", nil
	}
	if info.Size() == tab.offset {
		return "", nil
	}
	tab.file.Seek(tab.offset, io.SeekStart)
	data, err := io.ReadAll(io.LimitReader(tab.file, info.Size()-tab.offset))
	if err != nil {
		return "", err
	}
	tab.offset += int64(len(data))
	return splitTrailingPartial(tab, tab.buf+string(data)), nil
}

// openTabFile lazy-opens the file behind tab and seeks to the start of
// the last initialBackfillLines lines. Subsequent reads continue from
// wherever the previous read stopped via tab.offset.
func openTabFile(tab *tabState) error {
	if tab.file != nil {
		return nil
	}
	f, err := os.Open(tab.logPath)
	if err != nil {
		return err
	}
	tab.file = f
	off, err := seekToLastNLines(f, initialBackfillLines)
	if err != nil {
		return err
	}
	tab.offset = off
	return nil
}

// seekToLastNLines returns the byte offset that starts the last n lines of
// f (counting both terminated lines and a final unterminated line). If the
// file has fewer than n lines, or n <= 0, it returns 0. The file's seek
// position is left unchanged.
func seekToLastNLines(f *os.File, n int) (int64, error) {
	if n <= 0 {
		return 0, nil
	}
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := info.Size()
	if size == 0 {
		return 0, nil
	}

	// Detect a trailing newline: when present, the final \n doesn't open
	// a new line, so the (n+1)-th newline from the end marks the start of
	// the desired suffix. When absent, the trailing partial line counts
	// as line #1 from the end, so we only need n newlines back.
	lastByte := make([]byte, 1)
	if _, err := f.ReadAt(lastByte, size-1); err != nil {
		return 0, err
	}
	target := n
	if lastByte[0] == '\n' {
		target = n + 1
	}

	buf := make([]byte, tailReadChunk)
	pos := size
	newlines := 0
	for pos > 0 {
		readSize := int64(len(buf))
		if pos < readSize {
			readSize = pos
		}
		pos -= readSize
		if _, err := f.ReadAt(buf[:readSize], pos); err != nil {
			return 0, err
		}
		for i := readSize - 1; i >= 0; i-- {
			if buf[i] != '\n' {
				continue
			}
			newlines++
			if newlines >= target {
				return pos + i + 1, nil
			}
		}
	}
	return 0, nil
}

func splitTrailingPartial(tab *tabState, text string) string {
	if strings.HasSuffix(text, "\n") {
		tab.buf = ""
		return strings.TrimRight(text, "\n")
	}
	lastNL := strings.LastIndex(text, "\n")
	if lastNL < 0 {
		tab.buf = text
		return ""
	}
	tab.buf = text[lastNL+1:]
	return text[:lastNL]
}

func trimLines(content string, max int) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= max {
		return content
	}
	return strings.Join(lines[len(lines)-max:], "\n")
}

func tickCmd() tea.Cmd {
	return tea.Tick(tailPollInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

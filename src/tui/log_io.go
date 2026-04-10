package tui

import (
	"os"
	"strings"
)

// seekToLastNLines returns the byte offset that starts the last n lines of
// f (counting both terminated lines and a final unterminated line). If the
// file has fewer than n lines, or n <= 0, it returns 0. The file's seek
// position is left unchanged.
//
// Used for initial backfill on tab switch — the TUI reads the tail of the
// file once at activation, then relies on daemon push events for new content.
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

func trimLines(content string, max int) string {
	lines := strings.Split(content, "\n")
	if len(lines) <= max {
		return content
	}
	return strings.Join(lines[len(lines)-max:], "\n")
}

package runtime

import (
	"log/slog"
	"strings"
)

const paneSnapshotLines = 40

func (r *Runtime) logPaneSnapshot(reason, stage, target string) {
	snap, err := r.cfg.Tmux.InspectPane(target, paneSnapshotLines)
	if err != nil {
		slog.Debug("runtime: pane snapshot failed",
			"reason", reason,
			"stage", stage,
			"target", target,
			"err", err)
		return
	}
	slog.Debug("runtime: pane snapshot",
		"reason", reason,
		"stage", stage,
		"target", snap.Target,
		"dead", snap.Dead,
		"in_mode", snap.InMode,
		"command", snap.CurrentCommand,
		"cursor_x", snap.CursorX,
		"cursor_y", snap.CursorY,
		"tail", compactPaneTail(snap.ContentTail))
}

func compactPaneTail(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 8 {
		lines = lines[len(lines)-8:]
	}
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.Join(lines, " | ")
}

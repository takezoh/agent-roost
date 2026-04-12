package transcript

import (
	"encoding/json"
	"fmt"
	"strings"
)

type BashResult struct {
	StdoutLines int
	StderrLines int
	StdoutHead  []string // up to a few leading non-empty stdout lines
	Interrupted bool
}

func (BashResult) isToolResult() {}

func (r BashResult) Summary() string {
	var parts []string
	if r.Interrupted {
		parts = append(parts, "interrupted")
	}
	switch {
	case r.StdoutLines > 0:
		parts = append(parts, fmt.Sprintf("%d lines stdout", r.StdoutLines))
	case r.StderrLines == 0 && !r.Interrupted:
		parts = append(parts, "ok")
	}
	if r.StderrLines > 0 {
		parts = append(parts, fmt.Sprintf("%d lines stderr", r.StderrLines))
	}
	if len(parts) == 0 {
		return "ok"
	}
	return strings.Join(parts, ", ")
}

func parseBashResult(raw json.RawMessage) BashResult {
	var v struct {
		Stdout      string `json:"stdout"`
		Stderr      string `json:"stderr"`
		Interrupted bool   `json:"interrupted"`
	}
	_ = json.Unmarshal(raw, &v)
	out := BashResult{
		StdoutLines: countLines(v.Stdout),
		StderrLines: countLines(v.Stderr),
		Interrupted: v.Interrupted,
		StdoutHead:  leadingLines(v.Stdout, 3, 160),
	}
	return out
}

package transcript

import (
	"encoding/json"
	"fmt"
)

type EditResult struct {
	FilePath     string
	AddedLines   int
	RemovedLines int
	Hunks        int
}

func (EditResult) isToolResult() {}

func (r EditResult) Summary() string {
	if r.Hunks == 0 && r.AddedLines == 0 && r.RemovedLines == 0 {
		return "written"
	}
	return fmt.Sprintf("+%d -%d (%d hunks)", r.AddedLines, r.RemovedLines, r.Hunks)
}

func parseEditResult(raw json.RawMessage) EditResult {
	var v struct {
		FilePath        string `json:"filePath"`
		StructuredPatch []struct {
			Lines []string `json:"lines"`
		} `json:"structuredPatch"`
	}
	_ = json.Unmarshal(raw, &v)
	out := EditResult{FilePath: v.FilePath, Hunks: len(v.StructuredPatch)}
	for _, h := range v.StructuredPatch {
		for _, l := range h.Lines {
			if len(l) == 0 {
				continue
			}
			switch l[0] {
			case '+':
				out.AddedLines++
			case '-':
				out.RemovedLines++
			}
		}
	}
	return out
}

package transcript

import (
	"encoding/json"
	"fmt"
)

type ReadResult struct {
	FilePath   string
	StartLine  int
	NumLines   int
	TotalLines int
}

func (ReadResult) isToolResult() {}

func (r ReadResult) Summary() string {
	if r.NumLines == 0 && r.TotalLines == 0 {
		return "read"
	}
	if r.TotalLines > 0 && r.NumLines > 0 {
		end := r.StartLine + r.NumLines - 1
		if r.StartLine == 0 {
			end = r.NumLines
		}
		return fmt.Sprintf("lines %d-%d / %d", maxInt(r.StartLine, 1), end, r.TotalLines)
	}
	return fmt.Sprintf("%d lines", r.NumLines)
}

func parseReadResult(raw json.RawMessage) ReadResult {
	var v struct {
		File struct {
			FilePath   string `json:"filePath"`
			StartLine  int    `json:"startLine"`
			NumLines   int    `json:"numLines"`
			TotalLines int    `json:"totalLines"`
		} `json:"file"`
	}
	_ = json.Unmarshal(raw, &v)
	return ReadResult{
		FilePath:   v.File.FilePath,
		StartLine:  v.File.StartLine,
		NumLines:   v.File.NumLines,
		TotalLines: v.File.TotalLines,
	}
}

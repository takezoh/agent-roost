package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeJSONL(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func appendJSONL(t *testing.T, path, body string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(body); err != nil {
		t.Fatalf("append: %v", err)
	}
}

func TestTracker_AccumulatesUsageAndInsight(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeJSONL(t, path, `{"type":"assistant","message":{"model":"claude-opus-4-6","usage":{"input_tokens":1000,"output_tokens":200},"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}
`)

	tr := NewTracker()
	line, changed := tr.Update("sess", path)
	if !changed {
		t.Fatal("expected first update to change line")
	}
	if !strings.Contains(line, "opus-4-6") {
		t.Errorf("missing model: %q", line)
	}
	if !strings.Contains(line, "1k↓") {
		t.Errorf("missing input tokens: %q", line)
	}
	if !strings.Contains(line, "▸ Bash") {
		t.Errorf("missing current tool: %q", line)
	}

	// Append a tool_result that clears CurrentTool and adds another usage row
	appendJSONL(t, path, `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"a"}]}}
{"type":"assistant","message":{"model":"claude-opus-4-6","usage":{"input_tokens":500,"output_tokens":100},"content":[{"type":"text","text":"done"}]}}
`)
	line2, changed := tr.Update("sess", path)
	if !changed {
		t.Fatal("expected second update to change line")
	}
	if strings.Contains(line2, "▸ Bash") {
		t.Errorf("CurrentTool should be cleared: %q", line2)
	}
	if !strings.Contains(line2, "1k↓") {
		// Tokens accumulate: 1000 + 500 = 1500 -> still "1k"
		t.Errorf("tokens not accumulating: %q", line2)
	}
}

func TestTracker_NoChangeOnIdleUpdate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeJSONL(t, path, `{"type":"assistant","message":{"model":"claude-opus-4-6","usage":{"input_tokens":100,"output_tokens":50},"content":[]}}
`)
	tr := NewTracker()
	tr.Update("sess", path)
	_, changed := tr.Update("sess", path)
	if changed {
		t.Errorf("idle re-read should not report change")
	}
}

func TestTracker_EmptyPath(t *testing.T) {
	tr := NewTracker()
	if line, changed := tr.Update("sess", ""); line != "" || changed {
		t.Errorf("empty path should yield no change, got %q changed=%v", line, changed)
	}
}

func TestTracker_TracksErrorsAndSubagents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeJSONL(t, path, `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Agent","input":{"description":"x","subagent_type":"Explore"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"done"}]},"toolUseResult":{"agentId":"a","agentType":"Explore","status":"completed","totalDurationMs":1000}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"oops"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":"err","is_error":true}]}}
`)
	tr := NewTracker()
	line, _ := tr.Update("sess", path)
	if !strings.Contains(line, "1 err") {
		t.Errorf("error count missing: %q", line)
	}
	if !strings.Contains(line, "1 subs") {
		t.Errorf("subagent count missing: %q", line)
	}
}

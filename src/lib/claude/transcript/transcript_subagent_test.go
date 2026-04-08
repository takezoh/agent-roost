package transcript

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestSubagentLoader_Basic(t *testing.T) {
	fsys := fstest.MapFS{
		"agent-abc.jsonl": &fstest.MapFile{Data: []byte(
			`{"type":"user","message":{"content":"explore"}}` + "\n" +
				`{"type":"assistant","message":{"content":[{"type":"text","text":"found it"}]}}` + "\n",
		)},
		"agent-abc.meta.json": &fstest.MapFile{Data: []byte(`{"agentType":"Explore","description":"look around"}`)},
	}
	loader := NewSubagentLoader(fsys, ".", ParserOptions{})
	entries := loader.Load("abc", 0)
	if len(entries) < 4 {
		t.Fatalf("expected begin + body + end, got %d entries: %+v", len(entries), entries)
	}
	if entries[0].Kind != KindSubagentBegin {
		t.Errorf("first kind = %v, want SubagentBegin", entries[0].Kind)
	}
	if !strings.Contains(entries[0].Text, "Explore") {
		t.Errorf("begin label = %q", entries[0].Text)
	}
	if entries[len(entries)-1].Kind != KindSubagentEnd {
		t.Errorf("last kind = %v, want SubagentEnd", entries[len(entries)-1].Kind)
	}
}

func TestSubagentLoader_DepthBaseShift(t *testing.T) {
	fsys := fstest.MapFS{
		"agent-x.jsonl": &fstest.MapFile{Data: []byte(
			`{"type":"user","message":{"content":"hi"}}` + "\n",
		)},
		"agent-x.meta.json": &fstest.MapFile{Data: []byte(`{"agentType":"Plan"}`)},
	}
	loader := NewSubagentLoader(fsys, ".", ParserOptions{})
	got := loader.Load("x", 2)
	for _, e := range got {
		if e.Depth < 2 {
			t.Errorf("entry %v has depth %d, want >= 2", e.Kind, e.Depth)
		}
	}
}

func TestSubagentLoader_MissingFile(t *testing.T) {
	loader := NewSubagentLoader(fstest.MapFS{}, ".", ParserOptions{})
	if got := loader.Load("nope", 0); got != nil {
		t.Errorf("missing file should return nil, got %+v", got)
	}
}

func TestSubagentLoader_CacheReuse(t *testing.T) {
	fsys := fstest.MapFS{
		"agent-cached.jsonl":      &fstest.MapFile{Data: []byte(`{"type":"user","message":{"content":"x"}}` + "\n")},
		"agent-cached.meta.json":  &fstest.MapFile{Data: []byte(`{"agentType":"A"}`)},
	}
	loader := NewSubagentLoader(fsys, ".", ParserOptions{})
	first := loader.Load("cached", 0)
	second := loader.Load("cached", 0)
	if len(first) != len(second) {
		t.Errorf("cache miss: %d vs %d", len(first), len(second))
	}
}

func TestParser_InlineSubagentExpansion(t *testing.T) {
	subFsys := fstest.MapFS{
		"agent-aaa111.jsonl": &fstest.MapFile{Data: []byte(
			`{"type":"user","message":{"content":"investigate transcript"}}` + "\n" +
				`{"type":"assistant","message":{"content":[{"type":"text","text":"found schema"}]}}` + "\n",
		)},
		"agent-aaa111.meta.json": &fstest.MapFile{Data: []byte(`{"agentType":"Explore","description":"look at jsonl"}`)},
	}
	p := NewParser(ParserOptions{SubagentFS: subFsys, SubagentDir: "."})

	// 1) assistant tool_use registers tool_use_id -> Agent
	asst := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Agent","input":{"description":"investigate transcript","subagent_type":"Explore"}}]}}`
	p.ParseLines([]byte(asst))

	// 2) user tool_result with toolUseResult.agentId triggers inline expansion
	user := `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"done"}]},"toolUseResult":{"agentId":"aaa111","agentType":"Explore","status":"completed","totalDurationMs":12345}}`
	entries := p.ParseLines([]byte(user))

	// Expect: tool_result entry, then SubagentBegin + body + SubagentEnd
	if len(entries) < 4 {
		t.Fatalf("expected inlined subagent entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].Kind != KindToolResult {
		t.Errorf("entries[0] = %v, want ToolResult", entries[0].Kind)
	}
	if entries[1].Kind != KindSubagentBegin {
		t.Errorf("entries[1] = %v, want SubagentBegin", entries[1].Kind)
	}
	// Subagent body should have depth >= 1 (relative to the parent line).
	hasBody := false
	for _, e := range entries[1:] {
		if e.Depth >= 1 {
			hasBody = true
		}
	}
	if !hasBody {
		t.Errorf("subagent body should have depth >= 1")
	}

	// Render exercises the depth indent code path.
	out := RenderEntries(entries)
	if !strings.Contains(out, "Explore") {
		t.Errorf("render missing Explore: %q", out)
	}
	if !strings.Contains(out, "found schema") {
		t.Errorf("render missing subagent text: %q", out)
	}
}

func TestParser_InlineSubagent_NoLoaderNoExpansion(t *testing.T) {
	p := NewParser(ParserOptions{}) // no SubagentFS
	p.ParseLines([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Agent","input":{"description":"x"}}]}}`))
	entries := p.ParseLines([]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":""}]},"toolUseResult":{"agentId":"abc","status":"completed"}}`))
	if len(entries) != 1 {
		t.Errorf("without loader expected 1 entry (just tool_result), got %d: %+v", len(entries), entries)
	}
}

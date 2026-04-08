package transcript

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseToolInput_Bash(t *testing.T) {
	in := ParseToolInput("Bash", json.RawMessage(`{"command":"go test ./...","description":"run tests"}`))
	if in.Primary != "go test ./..." {
		t.Errorf("Primary = %q", in.Primary)
	}
	if in.Detail != "run tests" {
		t.Errorf("Detail = %q", in.Detail)
	}
}

func TestParseToolInput_ReadEdit(t *testing.T) {
	in := ParseToolInput("Read", json.RawMessage(`{"file_path":"src/main.go"}`))
	if in.Primary != "src/main.go" {
		t.Errorf("Read Primary = %q", in.Primary)
	}
	in = ParseToolInput("Edit", json.RawMessage(`{"file_path":"a.go","old_string":"x","new_string":"y"}`))
	if in.Primary != "a.go" {
		t.Errorf("Edit Primary = %q", in.Primary)
	}
}

func TestParseToolInput_MultiEdit(t *testing.T) {
	in := ParseToolInput("MultiEdit", json.RawMessage(`{"file_path":"a.go","edits":[{"old_string":"x","new_string":"y"},{"old_string":"p","new_string":"q"}]}`))
	if in.Primary != "a.go" {
		t.Errorf("Primary = %q", in.Primary)
	}
	if in.Detail != "2 edits" {
		t.Errorf("Detail = %q, want '2 edits'", in.Detail)
	}
}

func TestParseToolInput_GrepGlob(t *testing.T) {
	in := ParseToolInput("Grep", json.RawMessage(`{"pattern":"func main","path":"src"}`))
	if in.Primary != "func main" || in.Detail != "src" {
		t.Errorf("got %+v", in)
	}
}

func TestParseToolInput_Agent(t *testing.T) {
	in := ParseToolInput("Agent", json.RawMessage(`{"description":"explore codebase","subagent_type":"Explore","prompt":"look at src"}`))
	if in.Primary != "explore codebase" {
		t.Errorf("Primary = %q", in.Primary)
	}
	if in.Detail != "Explore" {
		t.Errorf("Detail = %q", in.Detail)
	}
}

func TestParseToolInput_TodoWrite(t *testing.T) {
	in := ParseToolInput("TodoWrite", json.RawMessage(`{"todos":[{"status":"pending"},{"status":"in_progress"},{"status":"completed"},{"status":"completed"}]}`))
	if !strings.Contains(in.Primary, "1 pending") || !strings.Contains(in.Primary, "1 in_progress") || !strings.Contains(in.Primary, "2 completed") {
		t.Errorf("Primary = %q", in.Primary)
	}
}

func TestParseToolInput_Web(t *testing.T) {
	in := ParseToolInput("WebFetch", json.RawMessage(`{"url":"https://example.com","prompt":"summary"}`))
	if in.Primary != "https://example.com" {
		t.Errorf("WebFetch Primary = %q", in.Primary)
	}
	in = ParseToolInput("WebSearch", json.RawMessage(`{"query":"go generics"}`))
	if in.Primary != "go generics" {
		t.Errorf("WebSearch Primary = %q", in.Primary)
	}
}

func TestParseToolInput_MCP(t *testing.T) {
	in := ParseToolInput("mcp__filesystem__read_text_file", json.RawMessage(`{"path":"/tmp/foo.txt"}`))
	if in.Primary != "/tmp/foo.txt" {
		t.Errorf("MCP Primary = %q", in.Primary)
	}
	if in.Detail != "filesystem" {
		t.Errorf("MCP Detail = %q", in.Detail)
	}
}

func TestParseToolInput_Unknown(t *testing.T) {
	in := ParseToolInput("CustomTool", json.RawMessage(`{"x":"y"}`))
	if in.Primary != "" || in.Detail != "" {
		t.Errorf("unknown tool should return empty, got %+v", in)
	}
}

func TestParseToolUseResult_Bash(t *testing.T) {
	raw := json.RawMessage(`{"stdout":"hello\nworld\n","stderr":"","interrupted":false}`)
	r, ok := ParseToolUseResult("Bash", raw).(BashResult)
	if !ok {
		t.Fatalf("expected BashResult, got %T", ParseToolUseResult("Bash", raw))
	}
	if r.StdoutLines != 2 {
		t.Errorf("StdoutLines = %d", r.StdoutLines)
	}
	if r.StderrLines != 0 {
		t.Errorf("StderrLines = %d", r.StderrLines)
	}
	if !strings.Contains(r.Summary(), "2 lines stdout") {
		t.Errorf("Summary = %q", r.Summary())
	}
}

func TestParseToolUseResult_BashWithStderr(t *testing.T) {
	raw := json.RawMessage(`{"stdout":"ok\n","stderr":"warn1\nwarn2\n","interrupted":false}`)
	r := ParseToolUseResult("Bash", raw).(BashResult)
	if r.StderrLines != 2 {
		t.Errorf("StderrLines = %d", r.StderrLines)
	}
	if !strings.Contains(r.Summary(), "stderr") {
		t.Errorf("Summary = %q", r.Summary())
	}
}

func TestParseToolUseResult_Edit(t *testing.T) {
	raw := json.RawMessage(`{"filePath":"a.go","structuredPatch":[{"oldStart":1,"oldLines":2,"newStart":1,"newLines":3,"lines":[" ctx","-old1","-old2","+new1","+new2","+new3"]}]}`)
	r := ParseToolUseResult("Edit", raw).(EditResult)
	if r.AddedLines != 3 || r.RemovedLines != 2 || r.Hunks != 1 {
		t.Errorf("got %+v", r)
	}
	if !strings.Contains(r.Summary(), "+3 -2") {
		t.Errorf("Summary = %q", r.Summary())
	}
}

func TestParseToolUseResult_Read(t *testing.T) {
	raw := json.RawMessage(`{"type":"file","file":{"filePath":"a.go","startLine":1,"numLines":200,"totalLines":543}}`)
	r := ParseToolUseResult("Read", raw).(ReadResult)
	if r.NumLines != 200 || r.TotalLines != 543 {
		t.Errorf("got %+v", r)
	}
	if !strings.Contains(r.Summary(), "543") {
		t.Errorf("Summary = %q", r.Summary())
	}
}

func TestParseToolUseResult_GlobGrep(t *testing.T) {
	raw := json.RawMessage(`{"filenames":["a","b","c"],"mode":"files_with_matches","numFiles":3,"numLines":12}`)
	r := ParseToolUseResult("Grep", raw).(GlobGrepResult)
	if r.NumFiles != 3 || r.NumLines != 12 {
		t.Errorf("got %+v", r)
	}
}

func TestParseToolUseResult_Agent(t *testing.T) {
	raw := json.RawMessage(`{"agentId":"a1","agentType":"Explore","status":"completed","totalDurationMs":12345,"totalTokens":2500}`)
	r := ParseToolUseResult("Agent", raw).(AgentResult)
	if r.AgentType != "Explore" || r.Status != "completed" {
		t.Errorf("got %+v", r)
	}
	s := r.Summary()
	if !strings.Contains(s, "Explore") || !strings.Contains(s, "completed") || !strings.Contains(s, "12.3s") {
		t.Errorf("Summary = %q", s)
	}
}

func TestParseToolUseResult_UnknownFallsBackToGeneric(t *testing.T) {
	raw := json.RawMessage(`"some raw string"`)
	r := ParseToolUseResult("CustomTool", raw)
	if _, ok := r.(GenericResult); !ok {
		t.Errorf("expected GenericResult, got %T", r)
	}
}

func TestParseToolUseResult_Empty(t *testing.T) {
	if r := ParseToolUseResult("Bash", nil); r != nil {
		t.Errorf("empty raw should return nil, got %T", r)
	}
}

func TestParser_ToolUseIDLookup(t *testing.T) {
	p := NewParser(ParserOptions{})
	asst := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}`
	user := `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"a\nb\n"}]},"toolUseResult":{"stdout":"a\nb\n","stderr":"","interrupted":false}}`

	entries := p.ParseLines([]byte(asst))
	if len(entries) != 1 || entries[0].Kind != KindToolUse || entries[0].ToolName != "Bash" {
		t.Fatalf("assistant parse failed: %+v", entries)
	}

	entries = p.ParseLines([]byte(user))
	if len(entries) != 1 || entries[0].Kind != KindToolResult {
		t.Fatalf("user parse failed: %+v", entries)
	}
	if entries[0].ToolName != "Bash" {
		t.Errorf("ToolName = %q, want Bash (cross-line lookup)", entries[0].ToolName)
	}
	bash, ok := entries[0].ToolResult.(BashResult)
	if !ok {
		t.Fatalf("expected BashResult, got %T", entries[0].ToolResult)
	}
	if bash.StdoutLines != 2 {
		t.Errorf("StdoutLines = %d", bash.StdoutLines)
	}
}

func TestParser_ToolUseIDResetClears(t *testing.T) {
	p := NewParser(ParserOptions{})
	p.ParseLines([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"a.go"}}]}}`))
	p.Reset()
	entries := p.ParseLines([]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"x"}]},"toolUseResult":{"stdout":"ignored","interrupted":false}}`))
	if entries[0].ToolName != "" {
		t.Errorf("after Reset ToolName should be empty, got %q", entries[0].ToolName)
	}
	if _, ok := entries[0].ToolResult.(GenericResult); !ok {
		t.Errorf("after Reset should fall back to GenericResult, got %T", entries[0].ToolResult)
	}
}

func TestRenderToolUse_RichDisplay(t *testing.T) {
	p := NewParser(ParserOptions{})
	entries := p.ParseLines([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"make build","description":"compile"}}]}}`))
	out := RenderEntries(entries)
	if !strings.Contains(out, "Bash") || !strings.Contains(out, "make build") || !strings.Contains(out, "compile") {
		t.Errorf("render = %q", out)
	}
}

func TestRenderToolResult_BashStdoutHead(t *testing.T) {
	p := NewParser(ParserOptions{})
	p.ParseLines([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}`))
	entries := p.ParseLines([]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"a\nb\n"}]},"toolUseResult":{"stdout":"file1\nfile2\n","stderr":"","interrupted":false}}`))
	out := RenderEntries(entries)
	if !strings.Contains(out, "2 lines stdout") {
		t.Errorf("summary missing: %q", out)
	}
	if !strings.Contains(out, "file1") || !strings.Contains(out, "file2") {
		t.Errorf("stdout head missing: %q", out)
	}
}

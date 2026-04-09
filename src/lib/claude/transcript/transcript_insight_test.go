package transcript

import (
	"strings"
	"testing"
)

func TestAggregateMeta_TitleAndPrompt(t *testing.T) {
	p := NewParser(ParserOptions{})
	entries := p.ParseAll(strings.NewReader(`{"type":"custom-title","customTitle":"my-session"}
{"type":"user","message":{"content":"first prompt"}}
{"type":"last-prompt","lastPrompt":"first prompt"}
{"type":"user","message":{"content":"latest prompt"}}
{"type":"last-prompt","lastPrompt":"latest prompt"}
`))
	snap := AggregateMeta(entries)
	if snap.Title != "my-session" {
		t.Errorf("Title = %q", snap.Title)
	}
	if snap.LastPrompt != "latest prompt" {
		t.Errorf("LastPrompt = %q", snap.LastPrompt)
	}
}

func TestAggregateMeta_LastPromptIgnoresUserText(t *testing.T) {
	// KindUser entries must NOT contribute to LastPrompt — only the
	// last-prompt control event does. This guarantees rewound user-text
	// (still present in the JSONL after a rewind) cannot leak into the
	// session-meta view.
	p := NewParser(ParserOptions{})
	entries := p.ParseAll(strings.NewReader(`{"type":"user","message":{"content":"rewound prompt A"}}
{"type":"user","message":{"content":"rewound prompt B"}}
{"type":"last-prompt","lastPrompt":"active prompt"}
`))
	snap := AggregateMeta(entries)
	if snap.LastPrompt != "active prompt" {
		t.Errorf("LastPrompt = %q, want %q", snap.LastPrompt, "active prompt")
	}
}

func TestAggregateMeta_LastPromptRewindReverts(t *testing.T) {
	// After the user rewinds without resubmitting, Claude Code emits a
	// fresh last-prompt event pointing back to the rewind target.
	// AggregateMeta must surface that target, not whatever user-text
	// happens to be the latest line in the file.
	p := NewParser(ParserOptions{})
	entries := p.ParseAll(strings.NewReader(`{"type":"user","message":{"content":"prompt A"}}
{"type":"last-prompt","lastPrompt":"prompt A"}
{"type":"user","message":{"content":"prompt B"}}
{"type":"last-prompt","lastPrompt":"prompt B"}
{"type":"last-prompt","lastPrompt":"prompt A"}
`))
	snap := AggregateMeta(entries)
	if snap.LastPrompt != "prompt A" {
		t.Errorf("LastPrompt = %q, want %q", snap.LastPrompt, "prompt A")
	}
}

func TestAggregateMeta_NoLastPromptEvent(t *testing.T) {
	// Old transcripts without last-prompt events leave LastPrompt empty;
	// no fallback to KindUser.
	p := NewParser(ParserOptions{})
	entries := p.ParseAll(strings.NewReader(`{"type":"user","message":{"content":"hello"}}
{"type":"user","message":{"content":"world"}}
`))
	snap := AggregateMeta(entries)
	if snap.LastPrompt != "" {
		t.Errorf("LastPrompt = %q, want empty", snap.LastPrompt)
	}
}

func TestAggregateMeta_LastPromptEmptyString(t *testing.T) {
	// An empty lastPrompt value must reset LastPrompt — represents the
	// "rewind to a state with no user prompt yet" case.
	p := NewParser(ParserOptions{})
	entries := p.ParseAll(strings.NewReader(`{"type":"last-prompt","lastPrompt":"first"}
{"type":"last-prompt","lastPrompt":""}
`))
	snap := AggregateMeta(entries)
	if snap.LastPrompt != "" {
		t.Errorf("LastPrompt = %q, want empty", snap.LastPrompt)
	}
}

func TestAggregateMeta_TaskCreateSubjects(t *testing.T) {
	p := NewParser(ParserOptions{})
	entries := p.ParseAll(strings.NewReader(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"TaskCreate","input":{"subject":"Fix login"}}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"TaskCreate","input":{"subject":"Add tests"}}]}}
`))
	snap := AggregateMeta(entries)
	if len(snap.Subjects) != 2 || snap.Subjects[0] != "Fix login" || snap.Subjects[1] != "Add tests" {
		t.Errorf("Subjects = %v", snap.Subjects)
	}
}

func TestAggregateMeta_AgentNameAndCurrentTool(t *testing.T) {
	p := NewParser(ParserOptions{})
	entries := p.ParseAll(strings.NewReader(`{"type":"agent-name","agentName":"explorer"}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"a.go"}}]}}
`))
	snap := AggregateMeta(entries)
	if snap.Insight.AgentName != "explorer" {
		t.Errorf("AgentName = %q", snap.Insight.AgentName)
	}
	if snap.Insight.CurrentTool != "Read" {
		t.Errorf("CurrentTool = %q (should remain set when result hasn't arrived)", snap.Insight.CurrentTool)
	}
}

func TestAggregateMeta_CurrentToolClearedOnResult(t *testing.T) {
	p := NewParser(ParserOptions{})
	entries := p.ParseAll(strings.NewReader(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"a.go"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"x"}]}}
`))
	snap := AggregateMeta(entries)
	if snap.Insight.CurrentTool != "" {
		t.Errorf("CurrentTool should be cleared, got %q", snap.Insight.CurrentTool)
	}
}

func TestAggregateMeta_ErrorCount(t *testing.T) {
	p := NewParser(ParserOptions{})
	entries := p.ParseAll(strings.NewReader(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"oops"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"err","is_error":true}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"again"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":"err","is_error":true}]}}
`))
	snap := AggregateMeta(entries)
	if snap.Insight.ErrorCount != 2 {
		t.Errorf("ErrorCount = %d", snap.Insight.ErrorCount)
	}
}

func TestAggregateMeta_RecentCommandsAndTouchedFiles(t *testing.T) {
	p := NewParser(ParserOptions{})
	entries := p.ParseAll(strings.NewReader(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"pwd"}}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t3","name":"Read","input":{"file_path":"a.go"}}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t4","name":"Edit","input":{"file_path":"b.go"}}]}}
`))
	snap := AggregateMeta(entries)
	if len(snap.Insight.RecentCommands) != 2 {
		t.Errorf("RecentCommands = %v", snap.Insight.RecentCommands)
	}
	if len(snap.Insight.TouchedFiles) != 2 {
		t.Errorf("TouchedFiles = %v", snap.Insight.TouchedFiles)
	}
}

func TestAggregateMeta_SubagentCounts(t *testing.T) {
	p := NewParser(ParserOptions{})
	entries := p.ParseAll(strings.NewReader(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Agent","input":{"description":"x","subagent_type":"Explore"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"done"}]},"toolUseResult":{"agentId":"a","agentType":"Explore","status":"completed","totalDurationMs":1000}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t2","name":"Agent","input":{"description":"y","subagent_type":"Plan"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":"done"}]},"toolUseResult":{"agentId":"b","agentType":"Plan","status":"completed","totalDurationMs":2000}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t3","name":"Agent","input":{"description":"z","subagent_type":"Explore"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t3","content":"done"}]},"toolUseResult":{"agentId":"c","agentType":"Explore","status":"completed","totalDurationMs":3000}}
`))
	snap := AggregateMeta(entries)
	if snap.Insight.SubagentCounts["Explore"] != 2 {
		t.Errorf("Explore = %d", snap.Insight.SubagentCounts["Explore"])
	}
	if snap.Insight.SubagentCounts["Plan"] != 1 {
		t.Errorf("Plan = %d", snap.Insight.SubagentCounts["Plan"])
	}
	if snap.Insight.SubagentTotal() != 3 {
		t.Errorf("SubagentTotal = %d", snap.Insight.SubagentTotal())
	}
}

func TestUpdateInsight_Incremental(t *testing.T) {
	insight := SessionInsight{}
	p := NewParser(ParserOptions{})

	first := p.ParseLines([]byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}`))
	UpdateInsight(&insight, first)
	if insight.CurrentTool != "Bash" {
		t.Errorf("after first chunk CurrentTool = %q", insight.CurrentTool)
	}

	second := p.ParseLines([]byte(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"x"}]}}`))
	UpdateInsight(&insight, second)
	if insight.CurrentTool != "" {
		t.Errorf("after second chunk CurrentTool = %q", insight.CurrentTool)
	}
}

func TestAppendBoundedUnique(t *testing.T) {
	got := appendBoundedUnique(nil, "a", 3)
	got = appendBoundedUnique(got, "b", 3)
	got = appendBoundedUnique(got, "c", 3)
	got = appendBoundedUnique(got, "a", 3) // moves a to the end
	if len(got) != 3 || got[2] != "a" {
		t.Errorf("got %v", got)
	}
	got = appendBoundedUnique(got, "d", 3) // overflow drops oldest
	if len(got) != 3 || got[0] != "c" {
		t.Errorf("after overflow got %v", got)
	}
}

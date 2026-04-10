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
	changed, err := tr.Update("sess", path)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !changed {
		t.Fatal("expected first update to change line")
	}
	line := tr.StatusLine("sess")
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
	changed, err = tr.Update("sess", path)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !changed {
		t.Fatal("expected second update to change line")
	}
	line2 := tr.StatusLine("sess")
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
	changed, err := tr.Update("sess", path)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if changed {
		t.Errorf("idle re-read should not report change")
	}
}

func TestTracker_EmptyPath(t *testing.T) {
	tr := NewTracker()
	changed, err := tr.Update("sess", "")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if changed {
		t.Errorf("empty path should yield no change")
	}
	if line := tr.StatusLine("sess"); line != "" {
		t.Errorf("empty path should yield empty line, got %q", line)
	}
}

func TestTracker_TracksSubagents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeJSONL(t, path, `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Agent","input":{"description":"x","subagent_type":"Explore"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"done"}]},"toolUseResult":{"agentId":"a","agentType":"Explore","status":"completed","totalDurationMs":1000}}
`)
	tr := NewTracker()
	if _, err := tr.Update("sess", path); err != nil {
		t.Fatalf("update: %v", err)
	}
	line := tr.StatusLine("sess")
	if !strings.Contains(line, "1 subs") {
		t.Errorf("subagent count missing: %q", line)
	}
}

func TestTracker_TracksTitleAndLastPromptIncrementally(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeJSONL(t, path, `{"type":"custom-title","customTitle":"Initial title"}
{"type":"user","uuid":"u1","parentUuid":null,"message":{"content":"first prompt"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","message":{"content":[{"type":"text","text":"hello"}]}}
`)
	tr := NewTracker()
	if _, err := tr.Update("sess", path); err != nil {
		t.Fatalf("update: %v", err)
	}
	snap := tr.Snapshot("sess")
	if snap.Title != "Initial title" {
		t.Errorf("title = %q, want Initial title", snap.Title)
	}
	if snap.LastPrompt != "first prompt" {
		t.Errorf("lastPrompt = %q, want first prompt", snap.LastPrompt)
	}

	// Append: later user entry chained off the previous tail extends the
	// active branch and becomes the new lastPrompt.
	appendJSONL(t, path, `{"type":"custom-title","customTitle":"Updated title"}
{"type":"user","uuid":"u2","parentUuid":"a1","message":{"content":"second prompt"}}
`)
	if _, err := tr.Update("sess", path); err != nil {
		t.Fatalf("update: %v", err)
	}
	snap = tr.Snapshot("sess")
	if snap.Title != "Updated title" {
		t.Errorf("title after append = %q, want Updated title", snap.Title)
	}
	if snap.LastPrompt != "second prompt" {
		t.Errorf("lastPrompt after append = %q, want second prompt", snap.LastPrompt)
	}
}

func TestTracker_LastPromptFromUserChainIgnoresRewind(t *testing.T) {
	// Simulate rewind+resubmit: u1 → a1 → u2 (rewound, content "old"),
	// then user rewinds to a1 and resubmits as u3 (content "new") which
	// branches off the same parent a1. Tail is u3, so walking from u3
	// must surface "new", not "old".
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeJSONL(t, path, `{"type":"user","uuid":"u1","parentUuid":null,"message":{"content":"first"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","message":{"content":[{"type":"text","text":"reply"}]}}
{"type":"user","uuid":"u2","parentUuid":"a1","message":{"content":"old (rewound)"}}
{"type":"user","uuid":"u3","parentUuid":"a1","message":{"content":"new (resubmit)"}}
`)
	tr := NewTracker()
	if _, err := tr.Update("sess", path); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := tr.Snapshot("sess").LastPrompt; got != "new (resubmit)" {
		t.Errorf("lastPrompt = %q, want %q", got, "new (resubmit)")
	}
}

func TestTracker_LastPromptThroughAssistantTurns(t *testing.T) {
	// Tail is an assistant entry. Walking parent chain should hop through
	// it to find the user prompt that triggered it.
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeJSONL(t, path, `{"type":"user","uuid":"u1","parentUuid":null,"message":{"content":"the prompt"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","message":{"content":[{"type":"text","text":"part 1"}]}}
{"type":"assistant","uuid":"a2","parentUuid":"a1","message":{"content":[{"type":"text","text":"part 2"}]}}
`)
	tr := NewTracker()
	if _, err := tr.Update("sess", path); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := tr.Snapshot("sess").LastPrompt; got != "the prompt" {
		t.Errorf("lastPrompt = %q, want the prompt", got)
	}
}

func TestTracker_LastPromptIgnoresBashInputBlob(t *testing.T) {
	// `! pwd` injects <bash-input>pwd</bash-input> as a synthetic user
	// message. systemTagPrefixes strips it so the entry is dropped before
	// reaching the chain map; the prior real prompt remains the answer.
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeJSONL(t, path, `{"type":"user","uuid":"u1","parentUuid":null,"message":{"content":"real prompt"}}
{"type":"user","uuid":"u2","parentUuid":"u1","message":{"content":"<bash-input>pwd</bash-input>"}}
{"type":"user","uuid":"u3","parentUuid":"u2","message":{"content":"<bash-stdout>/workspace</bash-stdout>"}}
`)
	tr := NewTracker()
	if _, err := tr.Update("sess", path); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := tr.Snapshot("sess").LastPrompt; got != "real prompt" {
		t.Errorf("lastPrompt = %q, want real prompt (bash blobs must be filtered)", got)
	}
}

func TestTracker_LastPromptIgnoresSyntheticBlockText(t *testing.T) {
	// Block-text user content (skill bootstrap, interrupt markers,
	// command echoes) is always Synthetic. Tracker must skip it so the
	// previous real user prompt remains the answer.
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeJSONL(t, path, `{"type":"user","uuid":"u1","parentUuid":null,"message":{"content":"real prompt"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","message":{"content":[{"type":"text","text":"reply"}]}}
{"type":"user","uuid":"u2","parentUuid":"a1","message":{"content":[{"type":"text","text":"Base directory for this skill: /home/take/.claude/skills/commit"}]}}
`)
	tr := NewTracker()
	if _, err := tr.Update("sess", path); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := tr.Snapshot("sess").LastPrompt; got != "real prompt" {
		t.Errorf("lastPrompt = %q, want real prompt (synthetic block-text must be filtered)", got)
	}
}

func TestTracker_ChainStubKeepsWalkAcrossThinkingOnlyAssistant(t *testing.T) {
	// An assistant turn with only a `thinking` block produces no
	// displayable Entry, so parseLine emits a KindUnknown stub solely
	// to record uuid → parentUuid. Without it, walking from a later
	// tail would skip past the stubbed-out assistant and hit the
	// missing parent → return "".
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeJSONL(t, path, `{"type":"user","uuid":"u1","parentUuid":null,"message":{"content":"the prompt"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","message":{"content":[{"type":"thinking","thinking":"hidden","signature":"x"}]}}
{"type":"assistant","uuid":"a2","parentUuid":"a1","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}
`)
	tr := NewTracker()
	if _, err := tr.Update("sess", path); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := tr.Snapshot("sess").LastPrompt; got != "the prompt" {
		t.Errorf("lastPrompt = %q, want the prompt (chain walk must hop the thinking-only assistant)", got)
	}
}

func TestTracker_RewindWithoutResubmitStaysStale(t *testing.T) {
	// Pinning the documented edge case: when the user rewinds and never
	// submits, Claude Code does not write any marker, so the file's last
	// entry remains the rewound one. The tracker reflects that — there's
	// no signal to detect the rewind from the JSONL alone.
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeJSONL(t, path, `{"type":"user","uuid":"u1","parentUuid":null,"message":{"content":"first"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","message":{"content":[{"type":"text","text":"reply"}]}}
{"type":"user","uuid":"u2","parentUuid":"a1","message":{"content":"second (will be rewound)"}}
{"type":"assistant","uuid":"a2","parentUuid":"u2","message":{"content":[{"type":"text","text":"reply 2"}]}}
`)
	tr := NewTracker()
	if _, err := tr.Update("sess", path); err != nil {
		t.Fatalf("update: %v", err)
	}
	// Walking from a2 (tail) finds u2 first, not u1. Documented behavior.
	if got := tr.Snapshot("sess").LastPrompt; got != "second (will be rewound)" {
		t.Errorf("lastPrompt = %q, want second (will be rewound) — change of behavior intended?", got)
	}
}

func TestTracker_HandlesFileTruncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeJSONL(t, path, `{"type":"custom-title","customTitle":"original"}
{"type":"assistant","message":{"model":"claude-opus-4-6","usage":{"input_tokens":1000,"output_tokens":200},"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}
`)
	tr := NewTracker()
	if _, err := tr.Update("sess", path); err != nil {
		t.Fatalf("first update: %v", err)
	}
	if got := tr.Snapshot("sess").Title; got != "original" {
		t.Fatalf("title before truncate = %q, want original", got)
	}

	// Truncate by rewriting the file with a smaller body — this is what
	// happens after `claude --resume` rewinds to an earlier turn.
	writeJSONL(t, path, `{"type":"custom-title","customTitle":"rewound"}
`)
	if _, err := tr.Update("sess", path); err != nil {
		t.Fatalf("second update: %v", err)
	}
	snap := tr.Snapshot("sess")
	if snap.Title != "rewound" {
		t.Errorf("title after truncate = %q, want rewound", snap.Title)
	}
	// Insight must reset too — otherwise stale tool state persists.
	if snap.Insight.CurrentTool != "" {
		t.Errorf("insight should reset; currentTool = %q", snap.Insight.CurrentTool)
	}
}

func TestTracker_RecentRoundsWalksUserBoundaries(t *testing.T) {
	// Two user prompts with assistant text + tool-use loop in between.
	// userTurns=2 should return everything from u1 onwards. The synthetic
	// tool_result user lines and the pure tool_use entries must NOT appear.
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeJSONL(t, path, `{"type":"user","uuid":"u1","parentUuid":null,"message":{"content":"first prompt"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","message":{"content":[{"type":"text","text":"reading"},{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/x"}}]}}
{"type":"user","uuid":"r1","parentUuid":"a1","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"file body"}]}}
{"type":"assistant","uuid":"a2","parentUuid":"r1","message":{"content":[{"type":"text","text":"found it"}]}}
{"type":"user","uuid":"u2","parentUuid":"a2","message":{"content":"second prompt"}}
`)
	tr := NewTracker()
	if _, err := tr.Update("sess", path); err != nil {
		t.Fatalf("update: %v", err)
	}

	got := tr.RecentRounds("sess", 2)
	want := []TurnText{
		{Role: "user", Text: "first prompt"},
		{Role: "assistant", Text: "reading"},
		{Role: "assistant", Text: "found it"},
		{Role: "user", Text: "second prompt"},
	}
	if len(got) != len(want) {
		t.Fatalf("RecentRounds len = %d, want %d (%+v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("RecentRounds[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestTracker_RecentRoundsLimitsToK(t *testing.T) {
	// Three real user turns. RecentRounds(2) should drop the first one.
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeJSONL(t, path, `{"type":"user","uuid":"u1","parentUuid":null,"message":{"content":"one"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","message":{"content":[{"type":"text","text":"r1"}]}}
{"type":"user","uuid":"u2","parentUuid":"a1","message":{"content":"two"}}
{"type":"assistant","uuid":"a2","parentUuid":"u2","message":{"content":[{"type":"text","text":"r2"}]}}
{"type":"user","uuid":"u3","parentUuid":"a2","message":{"content":"three"}}
`)
	tr := NewTracker()
	if _, err := tr.Update("sess", path); err != nil {
		t.Fatalf("update: %v", err)
	}

	got := tr.RecentRounds("sess", 2)
	want := []TurnText{
		{Role: "user", Text: "two"},
		{Role: "assistant", Text: "r2"},
		{Role: "user", Text: "three"},
	}
	if len(got) != len(want) {
		t.Fatalf("RecentRounds len = %d, want %d (%+v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("RecentRounds[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestTracker_RecentRoundsEmptyOnUnknownSession(t *testing.T) {
	tr := NewTracker()
	if got := tr.RecentRounds("missing", 2); got != nil {
		t.Errorf("expected nil for unknown session, got %+v", got)
	}
}

func TestTracker_RecentRoundsResetOnTruncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeJSONL(t, path, `{"type":"user","uuid":"u1","parentUuid":null,"message":{"content":"first"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","message":{"content":[{"type":"text","text":"reply"}]}}
`)
	tr := NewTracker()
	if _, err := tr.Update("sess", path); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := tr.RecentRounds("sess", 1); len(got) != 2 {
		t.Fatalf("setup: expected 2 entries before truncation, got %+v", got)
	}
	// Truncate (rewind via /resume).
	writeJSONL(t, path, `{"type":"user","uuid":"u9","parentUuid":null,"message":{"content":"only"}}
`)
	if _, err := tr.Update("sess", path); err != nil {
		t.Fatalf("re-update: %v", err)
	}
	got := tr.RecentRounds("sess", 1)
	if len(got) != 1 || got[0].Text != "only" {
		t.Errorf("post-truncation RecentRounds = %+v, want only the new entry", got)
	}
}

func TestTracker_ForgetReleasesState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeJSONL(t, path, `{"type":"custom-title","customTitle":"x"}
`)
	tr := NewTracker()
	if _, err := tr.Update("sess", path); err != nil {
		t.Fatalf("update: %v", err)
	}
	if tr.Snapshot("sess").Title != "x" {
		t.Fatal("setup: title not stored")
	}
	tr.Forget("sess")
	if got := tr.Snapshot("sess"); got.Title != "" {
		t.Errorf("Forget did not clear state: %+v", got)
	}
	if line := tr.StatusLine("sess"); line != "" {
		t.Errorf("Forget did not clear status line: %q", line)
	}
}

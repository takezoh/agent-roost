package driver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/take/agent-roost/lib/claude/transcript"
)

// stubSummarizer is a goroutine-safe seam used to swap out the real haiku
// invocation in tests. It records every call and either returns a canned
// reply or blocks on a release channel so tests can observe the in-flight
// state.
type stubSummarizer struct {
	mu      sync.Mutex
	calls   []string
	reply   string
	err     error
	release chan struct{}
}

func (s *stubSummarizer) fn(_ context.Context, prompt string) (string, error) {
	if s.release != nil {
		<-s.release
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, prompt)
	return s.reply, s.err
}

func (s *stubSummarizer) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func (s *stubSummarizer) lastCall() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.calls) == 0 {
		return ""
	}
	return s.calls[len(s.calls)-1]
}

// withStubSummarizer swaps summarizeFn for a stub, restoring the original
// when the test cleans up.
func withStubSummarizer(t *testing.T, stub *stubSummarizer) {
	t.Helper()
	prev := summarizeFn
	summarizeFn = stub.fn
	t.Cleanup(func() { summarizeFn = prev })
}

// writeRoundsTranscript writes a JSONL with two real user prompts and an
// assistant turn between them so RecentRounds has interesting input.
func writeRoundsTranscript(t *testing.T) (path, sessionID string) {
	t.Helper()
	dir := t.TempDir()
	sessionID = "sess-summary"
	path = filepath.Join(dir, sessionID+".jsonl")
	body := `{"type":"user","uuid":"u1","parentUuid":null,"message":{"content":"explain the driver"}}
{"type":"assistant","uuid":"a1","parentUuid":"u1","message":{"content":[{"type":"text","text":"the driver lives at session/driver/claude_driver.go"}]}}
{"type":"user","uuid":"u2","parentUuid":"a1","message":{"content":"now add a summary feature"}}
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	return path, sessionID
}

func TestClaudeDriver_SummaryFiresOnUserPromptSubmit(t *testing.T) {
	stub := &stubSummarizer{reply: "セッション要約を実装中"}
	withStubSummarizer(t, stub)

	d := newClaude(t)
	path, sid := writeRoundsTranscript(t)

	consumed := d.HandleEvent(AgentEvent{
		Type:  AgentEventStateChange,
		State: "running",
		DriverState: map[string]string{
			"session_id":      sid,
			"transcript_path": path,
			"hook_event_json": `{"session_id":"` + sid + `","hook_event_name":"UserPromptSubmit","cwd":"/proj"}`,
		},
	})
	if !consumed {
		t.Fatal("event should be consumed")
	}

	// triggerSummaryAsync runs in a goroutine, so wait briefly for it.
	if !waitFor(t, 500*time.Millisecond, func() bool { return stub.callCount() == 1 }) {
		t.Fatalf("summarizer was not called within timeout (calls=%d)", stub.callCount())
	}
	prompt := stub.lastCall()
	if !strings.Contains(prompt, "explain the driver") {
		t.Errorf("prompt missing first user turn: %q", prompt)
	}
	if !strings.Contains(prompt, "now add a summary feature") {
		t.Errorf("prompt missing latest user turn: %q", prompt)
	}
	if !strings.Contains(prompt, "the driver lives at session/driver/claude_driver.go") {
		t.Errorf("prompt missing assistant turn: %q", prompt)
	}

	// Wait for the result to land on d.summary.
	if !waitFor(t, 500*time.Millisecond, func() bool {
		return d.View().Card.Subtitle == "セッション要約を実装中"
	}) {
		t.Errorf("summary did not propagate to Card.Subtitle, got %q", d.View().Card.Subtitle)
	}
}

func TestClaudeDriver_SummaryDoesNotFireOnPreToolUse(t *testing.T) {
	stub := &stubSummarizer{reply: "should not be called"}
	withStubSummarizer(t, stub)

	d := newClaude(t)
	path, sid := writeRoundsTranscript(t)

	d.HandleEvent(AgentEvent{
		Type:  AgentEventStateChange,
		State: "running",
		DriverState: map[string]string{
			"session_id":      sid,
			"transcript_path": path,
			"hook_event_json": `{"session_id":"` + sid + `","hook_event_name":"PreToolUse","tool_name":"Bash"}`,
		},
	})
	// Give any errant goroutine a chance to run.
	time.Sleep(50 * time.Millisecond)
	if stub.callCount() != 0 {
		t.Errorf("summarizer should not fire on PreToolUse, got %d calls", stub.callCount())
	}
}

func TestClaudeDriver_SummaryDropsOverlappingCalls(t *testing.T) {
	release := make(chan struct{})
	stub := &stubSummarizer{reply: "first", release: release}
	withStubSummarizer(t, stub)

	d := newClaude(t)
	path, sid := writeRoundsTranscript(t)
	ev := AgentEvent{
		Type:  AgentEventStateChange,
		State: "running",
		DriverState: map[string]string{
			"session_id":      sid,
			"transcript_path": path,
			"hook_event_json": `{"session_id":"` + sid + `","hook_event_name":"UserPromptSubmit"}`,
		},
	}

	// First call: enters the stub and blocks on `release`.
	d.HandleEvent(ev)
	if !waitFor(t, 500*time.Millisecond, func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.summarizing
	}) {
		t.Fatal("first summarizer should be in flight")
	}

	// Second call while first is still blocked: must be dropped.
	d.HandleEvent(ev)

	// Release the first call and let it complete.
	close(release)
	if !waitFor(t, 500*time.Millisecond, func() bool { return stub.callCount() == 1 }) {
		t.Fatalf("expected exactly 1 call, got %d", stub.callCount())
	}

	// Cleanly verify the in-flight flag cleared.
	if !waitFor(t, 500*time.Millisecond, func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return !d.summarizing
	}) {
		t.Error("summarizing flag did not clear after completion")
	}
}

func TestFormatSummaryPrompt_CollapsesConsecutiveAssistant(t *testing.T) {
	turns := []transcript.TurnText{
		{Role: "user", Text: "ask 1"},
		{Role: "assistant", Text: "first"},
		{Role: "assistant", Text: "second"},
		{Role: "assistant", Text: "third"},
		{Role: "user", Text: "ask 2"},
	}
	got := formatSummaryPrompt("prior summary", turns)

	// One [assistant] header (collapsed) and two [user] headers.
	if c := strings.Count(got, "[assistant]"); c != 1 {
		t.Errorf("expected 1 [assistant] header (collapsed), got %d in:\n%s", c, got)
	}
	if c := strings.Count(got, "[user]"); c != 2 {
		t.Errorf("expected 2 [user] headers, got %d in:\n%s", c, got)
	}
	for _, want := range []string{"first", "second", "third", "ask 1", "ask 2", "prior summary"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestFormatSummaryPrompt_OmitsPreviousSummaryWhenEmpty(t *testing.T) {
	turns := []transcript.TurnText{{Role: "user", Text: "hello"}}
	got := formatSummaryPrompt("", turns)
	if strings.Contains(got, "<previous_summary>") {
		t.Errorf("empty prev should omit the block:\n%s", got)
	}
}

func TestFormatSummaryPrompt_TailClipsLongEntries(t *testing.T) {
	long := strings.Repeat("x", summaryEntryTextCap+500)
	turns := []transcript.TurnText{
		{Role: "user", Text: "head"},
		{Role: "assistant", Text: long},
	}
	got := formatSummaryPrompt("", turns)
	// The clip prefix marker must appear, signalling truncation happened.
	if !strings.Contains(got, "…") {
		t.Errorf("expected tail-clip marker in oversized entry:\n%s", got[:200])
	}
}

func TestClaudeDriver_PersistedStateIncludesSummary(t *testing.T) {
	d := newClaude(t)
	d.RestorePersistedState(map[string]string{
		"session_id": "abc",
		claudeKeySummary: "前回の要約",
	})
	out := d.PersistedState()
	if out[claudeKeySummary] != "前回の要約" {
		t.Errorf("summary did not round-trip: %+v", out)
	}
	if d.View().Card.Subtitle != "前回の要約" {
		t.Errorf("restored summary should appear as Subtitle, got %q", d.View().Card.Subtitle)
	}
}

// waitFor polls predicate every 5ms up to d, returning true on success.
// Used to wait on the goroutine-driven summarizer settling.
func waitFor(t *testing.T, d time.Duration, predicate func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if predicate() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return predicate()
}

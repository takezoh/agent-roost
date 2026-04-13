package transcript

import (
	"strings"
	"testing"
)

func parse(line string) (string, Snapshot) {
	p := NewParser()
	out := RenderEntries(p.ParseLines([]byte(line)))
	return out, p.Snapshot()
}

func TestParserUserMessage(t *testing.T) {
	got, snap := parse(`{"timestamp":"2025-09-28T12:11:06.932Z","type":"event_msg","payload":{"type":"user_message","message":"Run tests","kind":"plain"}}`)
	if !strings.Contains(got, "[context] Run tests") {
		t.Fatalf("got %q", got)
	}
	if snap.LastPrompt != "Run tests" {
		t.Fatalf("LastPrompt = %q", snap.LastPrompt)
	}
	if snap.Title != "Run tests" {
		t.Fatalf("Title = %q", snap.Title)
	}
}

func TestParserAssistantMessage(t *testing.T) {
	got, snap := parse(`{"timestamp":"2025-09-28T12:11:58.004Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}}`)
	if got != "" {
		t.Fatalf("got %q, want empty render", got)
	}
	if snap.LastAssistantMessage != "done" {
		t.Fatalf("LastAssistantMessage = %q", snap.LastAssistantMessage)
	}
}

func TestParserToolCall(t *testing.T) {
	got, _ := parse(strings.Join([]string{
		`{"timestamp":"2025-09-28T12:11:06.932Z","type":"event_msg","payload":{"type":"user_message","message":"Run tests","kind":"plain"}}`,
		`{"timestamp":"2025-09-28T12:11:58.004Z","type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"command\":\"go test ./...\"}"}}`,
	}, "\n"))
	if !strings.Contains(got, `▸ shell`) || !strings.Contains(got, `Run tests`) {
		t.Fatalf("got %q", got)
	}
}

func TestParserTurnAndRollbackEvents(t *testing.T) {
	p := NewParser()
	out := RenderEntries(p.ParseLines([]byte(strings.Join([]string{
		`{"timestamp":"x","type":"event_msg","payload":{"type":"user_message","message":"Investigate failure","kind":"plain"}}`,
		`{"timestamp":"x","type":"event_msg","payload":{"type":"turn_started","turn_id":"turn-1"}}`,
		`{"timestamp":"x","type":"event_msg","payload":{"type":"turn_complete","turn_id":"turn-1"}}`,
		`{"timestamp":"x","type":"event_msg","payload":{"type":"thread_rolled_back","num_turns":1}}`,
	}, "\n"))))
	if !strings.Contains(out, `[turn] started turn-1 <- "Investigate failure"`) {
		t.Fatalf("missing turn started: %q", out)
	}
	if !strings.Contains(out, `[turn] complete turn-1 <- "Investigate failure"`) {
		t.Fatalf("missing turn complete: %q", out)
	}
	if !strings.Contains(out, `[rollback] 1 turn(s) <- "Investigate failure"`) {
		t.Fatalf("missing rollback: %q", out)
	}
}

func TestParserThreadNameUpdatedAndStatusLine(t *testing.T) {
	p := NewParser()
	_ = p.ParseLines([]byte(strings.Join([]string{
		`{"timestamp":"x","type":"turn_context","payload":{"model":"gpt-5-codex","cwd":"/repo","approval_policy":"default","summary":"auto"}}`,
		`{"timestamp":"x","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":7205},"model_context_window":272000}}}`,
		`{"timestamp":"x","type":"event_msg","payload":{"type":"thread_name_updated","thread_name":"saved-session"}}`,
	}, "\n")))
	snap := p.Snapshot()
	if snap.Title != "saved-session" {
		t.Fatalf("Title = %q", snap.Title)
	}
	if !strings.Contains(snap.StatusLine, "gpt-5-codex") || !strings.Contains(snap.StatusLine, "7,205 tok") {
		t.Fatalf("StatusLine = %q", snap.StatusLine)
	}
}

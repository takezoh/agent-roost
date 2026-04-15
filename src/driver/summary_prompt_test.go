package driver

import (
	"strings"
	"testing"
)

func TestFormatGenericSummaryPromptIncludesContent(t *testing.T) {
	prompt := formatGenericSummaryPrompt("", "", "", "diff --git a/foo.go b/foo.go")
	if !strings.Contains(prompt, "diff --git a/foo.go b/foo.go") {
		t.Errorf("prompt does not contain the content: %q", prompt)
	}
	if !strings.Contains(prompt, "<terminal_output>") {
		t.Errorf("prompt missing <terminal_output> tag")
	}
}

func TestFormatGenericSummaryPromptIncludesPreviousSummary(t *testing.T) {
	withPrev := formatGenericSummaryPrompt("prev summary text", "", "", "some output")
	if !strings.Contains(withPrev, "<previous_summary>") {
		t.Error("expected <previous_summary> block when prev is non-empty")
	}
	if !strings.Contains(withPrev, "prev summary text") {
		t.Error("expected previous summary text in prompt")
	}

	withoutPrev := formatGenericSummaryPrompt("", "", "", "some output")
	if strings.Contains(withoutPrev, "<previous_summary>") {
		t.Error("unexpected <previous_summary> block when prev is empty")
	}
}

func TestFormatGenericSummaryPromptClipsLargeContent(t *testing.T) {
	// Build content larger than summaryTotalCap runes
	large := strings.Repeat("x", summaryTotalCap+100)
	prompt := formatGenericSummaryPrompt("", "", "", large)
	// The clipped marker "…" must appear
	if !strings.Contains(prompt, "…") {
		t.Error("expected tailClip ellipsis for large content")
	}
}

func TestFormatGenericSummaryPromptDoesNotMentionAgent(t *testing.T) {
	prompt := formatGenericSummaryPrompt("", "", "", "htop output")
	for _, bad := range []string{"AI coding session", "user turn", "recent_turns", "coding session"} {
		if strings.Contains(prompt, bad) {
			t.Errorf("prompt contains agent-specific wording %q", bad)
		}
	}
	if !strings.Contains(prompt, "terminal session summarizer") {
		t.Error("prompt missing expected 'terminal session summarizer' wording")
	}
}

func TestFormatGenericSummaryPromptIncludesCommandAndWorkingDir(t *testing.T) {
	prompt := formatGenericSummaryPrompt("", "tig", "/workspace/agent-roost", "diff content")
	if !strings.Contains(prompt, "<command>\ntig\n</command>") {
		t.Errorf("prompt missing <command> block: %q", prompt)
	}
	if !strings.Contains(prompt, "<working_directory>\n/workspace/agent-roost\n</working_directory>") {
		t.Errorf("prompt missing <working_directory> block: %q", prompt)
	}
}

func TestFormatGenericSummaryPromptOmitsEmptyMetadata(t *testing.T) {
	prompt := formatGenericSummaryPrompt("", "", "", "some output")
	// Opening-tag-with-newline signals an actual block (the instruction
	// text may mention the tag names inline, so a bare "<command>" match
	// is not sufficient).
	if strings.Contains(prompt, "<command>\n") {
		t.Error("expected no <command> block when command is empty")
	}
	if strings.Contains(prompt, "<working_directory>\n") {
		t.Error("expected no <working_directory> block when workingDir is empty")
	}
}

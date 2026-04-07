package driver

import "testing"

func TestRegistryGet_Known(t *testing.T) {
	r := DefaultRegistry()
	d := r.Get("claude")
	if d.Name() != "claude" {
		t.Fatalf("got %q, want %q", d.Name(), "claude")
	}
}

func TestRegistryGet_Unknown(t *testing.T) {
	r := DefaultRegistry()
	d := r.Get("unknown-cmd")
	if d == nil {
		t.Fatal("expected non-nil fallback driver")
	}
}

func TestRegistryGet_KnownCommands(t *testing.T) {
	r := DefaultRegistry()
	for _, cmd := range []string{"claude", "gemini", "codex", "bash"} {
		d := r.Get(cmd)
		if d.Name() != cmd {
			t.Errorf("Get(%q).Name() = %q, want %q", cmd, d.Name(), cmd)
		}
	}
}

func TestRegistryCompiledPattern_NotNil(t *testing.T) {
	r := DefaultRegistry()
	for _, cmd := range []string{"claude", "gemini", "codex", "bash", "unknown"} {
		if r.CompiledPattern(cmd) == nil {
			t.Errorf("CompiledPattern(%q) returned nil", cmd)
		}
	}
}

func TestClaudePromptPattern_Matches(t *testing.T) {
	r := DefaultRegistry()
	p := r.CompiledPattern("claude")
	tests := []struct {
		input string
		want  bool
	}{
		{"❯ ", true},
		{"output\n❯ ", true},
		{"> ", true},
		{"just text", false},
	}
	for _, tt := range tests {
		got := p.MatchString(tt.input)
		if got != tt.want {
			t.Errorf("claude pattern MatchString(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestClaudePromptPattern_NoDollar(t *testing.T) {
	r := DefaultRegistry()
	p := r.CompiledPattern("claude")
	if p.MatchString("$ ") {
		t.Error("claude pattern should not match '$ '")
	}
}

func TestGenericPromptPattern_Matches(t *testing.T) {
	r := DefaultRegistry()
	p := r.CompiledPattern("bash")
	tests := []struct {
		input string
		want  bool
	}{
		{"$ ", true},
		{"> ", true},
		{"❯ ", true},
		{"compiling...", false},
	}
	for _, tt := range tests {
		got := p.MatchString(tt.input)
		if got != tt.want {
			t.Errorf("generic pattern MatchString(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestDriverDisplayName(t *testing.T) {
	r := DefaultRegistry()
	for _, cmd := range []string{"claude", "gemini", "codex", "bash"} {
		d := r.Get(cmd)
		if d.DisplayName() != cmd {
			t.Errorf("Get(%q).DisplayName() = %q, want %q", cmd, d.DisplayName(), cmd)
		}
	}
}

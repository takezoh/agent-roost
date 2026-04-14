package notify

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestXMLEscape(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"a&b", "a&amp;b"},
		{"<tag>", "&lt;tag&gt;"},
		{`say "hi"`, "say &quot;hi&quot;"},
		{"it's", "it&apos;s"},
		{"<a>&\"'</a>", "&lt;a&gt;&amp;&quot;&apos;&lt;/a&gt;"},
	}
	for _, tt := range tests {
		if got := xmlEscape(tt.input); got != tt.want {
			t.Errorf("xmlEscape(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEmbeddedScript_NotEmpty(t *testing.T) {
	s := string(notifyScript)
	if !strings.Contains(s, "param") {
		t.Error("embedded script should have param declaration")
	}
	if !strings.Contains(s, "ToastText02") {
		t.Error("embedded script should use ToastText02 template")
	}
	if !strings.Contains(s, "ToastNotificationManager") {
		t.Error("embedded script should call ToastNotificationManager")
	}
	if !strings.Contains(s, `"Roost"`) {
		t.Error(`embedded script should use "Roost" as notifier ID`)
	}
}

func TestWriteScriptTempFile(t *testing.T) {
	path, err := writeScriptTempFile()
	if err != nil {
		t.Fatalf("writeScriptTempFile: %v", err)
	}
	defer os.Remove(path)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(notifyScript) {
		t.Error("written file content does not match embedded script")
	}
	if !strings.HasSuffix(path, ".ps1") {
		t.Errorf("temp file should have .ps1 suffix, got %q", path)
	}
}

func TestSend_NoPowerShell(t *testing.T) {
	// With empty PATH, LookPath("powershell.exe") will fail → Send returns nil (no-op)
	t.Setenv("PATH", "")
	if err := Send(context.Background(), "title", "body"); err != nil {
		t.Errorf("Send with no powershell.exe should return nil, got: %v", err)
	}
}

package notify

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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

// TestInstallScript_WritesToDataDir verifies that installScript creates
// <dataDir>/scripts/notify.ps1 with the embedded content, and that calling
// it a second time overwrites the file idempotently.
func TestInstallScript_WritesToDataDir(t *testing.T) {
	if _, err := exec.LookPath("wslpath"); err != nil {
		t.Skip("wslpath not available; installScript requires WSL")
	}

	dir := t.TempDir()
	ctx := context.Background()

	winPath1, err := installScript(ctx, dir)
	if err != nil {
		t.Fatalf("installScript: %v", err)
	}
	if winPath1 == "" {
		t.Fatal("expected non-empty winPath")
	}

	scriptPath := filepath.Join(dir, "scripts", "notify.ps1")
	got, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(notifyScript) {
		t.Error("scripts/notify.ps1 content does not match embedded script")
	}

	// Second call must overwrite idempotently and return the same Windows path.
	winPath2, err := installScript(ctx, dir)
	if err != nil {
		t.Fatalf("installScript (2nd call): %v", err)
	}
	if winPath1 != winPath2 {
		t.Errorf("winPath changed between calls: %q vs %q", winPath1, winPath2)
	}
}

// TestInstallScript_CreatesSubdir verifies that installScript creates the
// scripts/ subdirectory even when it does not yet exist.
func TestInstallScript_CreatesSubdir(t *testing.T) {
	if _, err := exec.LookPath("wslpath"); err != nil {
		t.Skip("wslpath not available; installScript requires WSL")
	}

	dir := t.TempDir()
	ctx := context.Background()

	if _, err := installScript(ctx, dir); err != nil {
		t.Fatalf("installScript: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "scripts"))
	if err != nil {
		t.Fatalf("scripts/ subdir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("scripts/ should be a directory")
	}
}

// TestNew_NoPowerShell verifies that New returns a no-op Notifier (nil error)
// when powershell.exe is not on PATH.
func TestNew_NoPowerShell(t *testing.T) {
	t.Setenv("PATH", "")
	n, err := New(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("New with no powershell.exe should return nil error, got: %v", err)
	}
	if n == nil {
		t.Fatal("New should return non-nil Notifier")
	}
	if n.psPath != "" {
		t.Errorf("no-op Notifier should have empty psPath, got %q", n.psPath)
	}
}

// TestNotifier_Send_NoPowerShell verifies that Send on a no-op Notifier returns nil.
func TestNotifier_Send_NoPowerShell(t *testing.T) {
	n := &Notifier{} // no-op: psPath is ""
	if err := n.Send(context.Background(), "title", "body"); err != nil {
		t.Errorf("no-op Notifier.Send should return nil, got: %v", err)
	}
}

func TestNotifySendArgs(t *testing.T) {
	args := notifySendArgs("My Title", "Some body")
	wants := map[string]bool{
		"--app-name=roost":      false,
		"--icon=agent-roost":    false,
		"--category=im.received": false,
	}
	for _, a := range args {
		if _, ok := wants[a]; ok {
			wants[a] = true
		}
	}
	for flag, found := range wants {
		if !found {
			t.Errorf("notify-send args missing %q", flag)
		}
	}
	last2 := args[len(args)-2:]
	if last2[0] != "My Title" || last2[1] != "Some body" {
		t.Errorf("title/body not at end of args: %v", args)
	}
}

func TestEscapeAppleScript(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{`hello`, `hello`},
		{`say "hi"`, `say \"hi\"`},
		{`back\slash`, `back\\slash`},
		{`a "b" \c`, `a \"b\" \\c`},
	}
	for _, tc := range tests {
		got := escapeAppleScript(tc.in)
		if got != tc.want {
			t.Errorf("escapeAppleScript(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

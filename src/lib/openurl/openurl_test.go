package openurl

import (
	"reflect"
	"testing"
)

func TestCommandEmptyTarget(t *testing.T) {
	e := env{goos: "linux"}
	if _, _, err := e.command(""); err == nil {
		t.Fatal("expected error for empty target, got nil")
	}
}

func TestCommandByOS(t *testing.T) {
	cases := []struct {
		name    string
		env     env
		target  string
		wantCmd string
		wantArg []string
	}{
		{
			name:    "darwin passes path through to open",
			env:     env{goos: "darwin"},
			target:  "/home/user/proj",
			wantCmd: "open",
			wantArg: []string{"/home/user/proj"},
		},
		{
			name:    "windows hands path to explorer",
			env:     env{goos: "windows"},
			target:  `C:\Users\me`,
			wantCmd: "explorer.exe",
			wantArg: []string{`C:\Users\me`},
		},
		{
			name:    "linux non-WSL uses xdg-open",
			env:     env{goos: "linux", wsl: false},
			target:  "/home/user/proj",
			wantCmd: "xdg-open",
			wantArg: []string{"/home/user/proj"},
		},
		{
			name:    "linux non-WSL forwards URLs to xdg-open",
			env:     env{goos: "linux", wsl: false},
			target:  "https://example.com",
			wantCmd: "xdg-open",
			wantArg: []string{"https://example.com"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name, args, err := tc.env.command(tc.target)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if name != tc.wantCmd {
				t.Errorf("cmd = %q, want %q", name, tc.wantCmd)
			}
			if !reflect.DeepEqual(args, tc.wantArg) {
				t.Errorf("args = %v, want %v", args, tc.wantArg)
			}
		})
	}
}

func TestCommandUnsupportedOS(t *testing.T) {
	e := env{goos: "plan9"}
	if _, _, err := e.command("/tmp/x"); err == nil {
		t.Fatal("expected error for unsupported GOOS, got nil")
	}
}

func TestFileTargetToPath(t *testing.T) {
	cases := []struct {
		in     string
		wantP  string
		wantOk bool
	}{
		{"file://localhost/tmp/a", "/tmp/a", true},
		{"file:///tmp/b", "/tmp/b", true},
		{"/abs/c", "/abs/c", true},
		{"https://x.test", "", false},
		{"relative/path", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			p, ok := fileTargetToPath(tc.in)
			if ok != tc.wantOk || p != tc.wantP {
				t.Errorf("fileTargetToPath(%q) = (%q,%v), want (%q,%v)", tc.in, p, ok, tc.wantP, tc.wantOk)
			}
		})
	}
}

func TestCommandWSLFileURL(t *testing.T) {
	// Under WSL the command is always explorer.exe. wslpath may or may
	// not be available in the test environment; when it fails we fall
	// back to passing the original target. Either outcome is acceptable
	// — we only assert the executable and that we pass exactly one arg.
	e := env{goos: "linux", wsl: true}
	name, args, err := e.command("file://localhost/tmp/x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "explorer.exe" {
		t.Errorf("cmd = %q, want explorer.exe", name)
	}
	if len(args) != 1 {
		t.Errorf("args = %v, want exactly one", args)
	}
}

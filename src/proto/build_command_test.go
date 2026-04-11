package proto

import "testing"

func TestBuildCommandPreviewProject(t *testing.T) {
	cmd, err := BuildCommand("preview-project", nil)
	if err != nil {
		t.Fatal(err)
	}
	pp, ok := cmd.(CmdPreviewProject)
	if !ok {
		t.Fatalf("got %T, want CmdPreviewProject", cmd)
	}
	if pp.Project != "" {
		t.Errorf("project = %q, want empty", pp.Project)
	}
}

func TestBuildCommandPreviewProjectWithArg(t *testing.T) {
	cmd, err := BuildCommand("preview-project", map[string]string{"project": "/foo"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.(CmdPreviewProject).Project != "/foo" {
		t.Error("project not set")
	}
}

func TestBuildCommandFocusPaneRequiresArg(t *testing.T) {
	_, err := BuildCommand("focus-pane", nil)
	if err == nil {
		t.Fatal("expected error for missing pane arg")
	}
}

func TestBuildCommandUnknown(t *testing.T) {
	_, err := BuildCommand("bogus", nil)
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
}

package runtime

import (
	"os"
	"strings"
	"testing"
)

func TestFileEventLogAppendCreatesFile(t *testing.T) {
	dir := t.TempDir()
	el := NewFileEventLog(dir)
	defer el.CloseAll()

	if err := el.Append("abc", "first"); err != nil {
		t.Fatalf("append: %v", err)
	}
	data, err := os.ReadFile(el.Path("abc"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(data), "first") {
		t.Errorf("file = %q", string(data))
	}
}

func TestFileEventLogMultipleAppends(t *testing.T) {
	dir := t.TempDir()
	el := NewFileEventLog(dir)
	defer el.CloseAll()

	for i, line := range []string{"a", "b", "c"} {
		if err := el.Append("s1", line); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	data, _ := os.ReadFile(el.Path("s1"))
	for _, want := range []string{"a", "b", "c"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %q in %q", want, string(data))
		}
	}
}

func TestFileEventLogIsolatesSessions(t *testing.T) {
	dir := t.TempDir()
	el := NewFileEventLog(dir)
	defer el.CloseAll()

	el.Append("a", "alpha")
	el.Append("b", "beta")
	dataA, _ := os.ReadFile(el.Path("a"))
	dataB, _ := os.ReadFile(el.Path("b"))
	if !strings.Contains(string(dataA), "alpha") || strings.Contains(string(dataA), "beta") {
		t.Errorf("a = %q", string(dataA))
	}
	if !strings.Contains(string(dataB), "beta") || strings.Contains(string(dataB), "alpha") {
		t.Errorf("b = %q", string(dataB))
	}
}

func TestFileEventLogCloseReopens(t *testing.T) {
	dir := t.TempDir()
	el := NewFileEventLog(dir)
	defer el.CloseAll()

	el.Append("x", "first")
	el.Close("x")
	if err := el.Append("x", "second"); err != nil {
		t.Fatalf("append after close: %v", err)
	}
	data, _ := os.ReadFile(el.Path("x"))
	if !strings.Contains(string(data), "first") || !strings.Contains(string(data), "second") {
		t.Errorf("file = %q", string(data))
	}
}

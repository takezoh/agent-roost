package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func TestRunNotify_MissingFlags(t *testing.T) {
	err := RunNotify([]string{})
	if err == nil {
		t.Fatal("expected error when no flags provided")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("error = %q, want 'required'", err.Error())
	}
}

func TestRunNotify_StdoutFallback(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := notifyStdout("hello", "world")

	w.Close()
	os.Stdout = old

	if err != nil {
		t.Fatalf("notifyStdout error: %v", err)
	}

	var buf bytes.Buffer
	io.Copy(&buf, r)

	var got map[string]string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json decode: %v (output: %q)", err, buf.String())
	}
	if got["title"] != "hello" {
		t.Errorf("title = %q, want hello", got["title"])
	}
	if got["body"] != "world" {
		t.Errorf("body = %q, want world", got["body"])
	}
}

func TestRunNotify_Registered(t *testing.T) {
	if !Has("notify") {
		t.Error("notify subcommand not registered")
	}
}

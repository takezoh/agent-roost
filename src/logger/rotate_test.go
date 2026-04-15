package logger

import (
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRotateLogs_Basic(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "roost.log")

	if err := os.WriteFile(logPath, []byte("first run\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rotateLogs(logPath)

	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Error("roost.log should not exist after rotation")
	}
	data, err := os.ReadFile(logPath + ".1")
	if err != nil {
		t.Fatalf("roost.log.1 not found: %v", err)
	}
	if string(data) != "first run\n" {
		t.Errorf("unexpected content in .1: %q", string(data))
	}
}

func TestRotateLogs_Chain(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "roost.log")

	// Seed generations .1 and .2
	os.WriteFile(logPath+".1", []byte("gen1\n"), 0o644)
	os.WriteFile(logPath+".2", []byte("gen2\n"), 0o644)
	os.WriteFile(logPath, []byte("gen0\n"), 0o644)

	rotateLogs(logPath)

	for gen, want := range map[string]string{
		logPath + ".1": "gen0\n",
		logPath + ".2": "gen1\n",
		logPath + ".3": "gen2\n",
	} {
		data, err := os.ReadFile(gen)
		if err != nil {
			t.Fatalf("missing %s: %v", gen, err)
		}
		if string(data) != want {
			t.Errorf("%s: got %q, want %q", gen, string(data), want)
		}
	}
}

func TestRotateLogs_MaxRotations(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "roost.log")

	// Fill all generations
	for i := 1; i <= maxRotations; i++ {
		os.WriteFile(logPath+"."+itoa(i), []byte("old\n"), 0o644)
	}
	os.WriteFile(logPath, []byte("new\n"), 0o644)

	rotateLogs(logPath)

	oldest := logPath + "." + itoa(maxRotations+1)
	if _, err := os.Stat(oldest); !os.IsNotExist(err) {
		t.Errorf("generation %d should have been deleted", maxRotations+1)
	}
	// .maxRotations should now hold what was .maxRotations-1
	data, _ := os.ReadFile(logPath + "." + itoa(maxRotations))
	if string(data) != "old\n" {
		t.Errorf(".%d: got %q", maxRotations, string(data))
	}
}

// TestInitWithDataDir_DoesNotRotate verifies that InitWithDataDir never
// rotates on its own. A subprocess calling Init should append to the
// existing roost.log without moving it to .1.
func TestInitWithDataDir_DoesNotRotate(t *testing.T) {
	dir := t.TempDir()

	if err := InitWithDataDir("info", dir); err != nil {
		t.Fatal(err)
	}
	logPath := LogFilePath()
	if err := os.WriteFile(logPath, []byte("coordinator run\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	Close()

	// Second init (subprocess) — must NOT rotate.
	if err := InitWithDataDir("info", dir); err != nil {
		t.Fatal(err)
	}
	defer Close()

	if _, err := os.Stat(logPath + ".1"); !os.IsNotExist(err) {
		t.Error("roost.log.1 must not exist: subprocess Init must not rotate")
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "coordinator run\n" {
		t.Errorf("roost.log content changed unexpectedly: %q", string(data))
	}
}

// TestRotate_ThenInitWithDataDir verifies the coordinator boot sequence:
// Rotate moves the previous log to .1, then InitWithDataDir opens a fresh file.
func TestRotate_ThenInitWithDataDir(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "roost.log")

	if err := os.WriteFile(logPath, []byte("previous run\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	Rotate(dir)

	if err := InitWithDataDir("info", dir); err != nil {
		t.Fatal(err)
	}
	defer Close()

	data, err := os.ReadFile(logPath + ".1")
	if err != nil {
		t.Fatalf("roost.log.1 not found after Rotate: %v", err)
	}
	if string(data) != "previous run\n" {
		t.Errorf("unexpected content in .1: %q", string(data))
	}
}

// TestRotate_MissingFile verifies that Rotate on a directory with no
// existing log file does not panic or return an error.
func TestRotate_MissingFile(t *testing.T) {
	dir := t.TempDir()
	Rotate(dir) // must not panic
	if _, err := os.Stat(filepath.Join(dir, "roost.log.1")); !os.IsNotExist(err) {
		t.Error("no .1 file expected when roost.log does not exist")
	}
}

// TestCoordinatorSubprocessSequence simulates the multi-process boot:
//  1. Coordinator: Rotate + Init + write a log line.
//  2. Subprocess:  Init only (no Rotate) + write another line.
//
// Both lines must appear in the same roost.log; roost.log.1 must only
// contain the "previous run" content from before the coordinator boot.
func TestCoordinatorSubprocessSequence(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "roost.log")

	// Seed a "previous run" log to ensure Rotate works end-to-end.
	if err := os.WriteFile(logPath, []byte("previous run\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// --- Coordinator boot ---
	Rotate(dir)
	if err := InitWithDataDir("info", dir); err != nil {
		t.Fatal(err)
	}
	slog.Info("coordinator line")
	Close()

	// --- Subprocess init (no Rotate) ---
	if err := InitWithDataDir("info", dir); err != nil {
		t.Fatal(err)
	}
	slog.Info("subprocess line")
	Close()

	// roost.log must contain both lines.
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "coordinator line") {
		t.Errorf("coordinator line missing from roost.log: %q", content)
	}
	if !strings.Contains(content, "subprocess line") {
		t.Errorf("subprocess line missing from roost.log: %q", content)
	}

	// roost.log.1 must be the old "previous run" only.
	old, err := os.ReadFile(logPath + ".1")
	if err != nil {
		t.Fatalf("roost.log.1 not found: %v", err)
	}
	if string(old) != "previous run\n" {
		t.Errorf("roost.log.1 unexpected content: %q", string(old))
	}

	// roost.log.2 must not exist (only one rotation happened).
	if _, err := os.Stat(logPath + ".2"); !os.IsNotExist(err) {
		t.Error("roost.log.2 must not exist")
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

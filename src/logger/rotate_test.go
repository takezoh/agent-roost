package logger

import (
	"os"
	"path/filepath"
	"strconv"
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

func TestInitWithDataDir_Rotates(t *testing.T) {
	dir := t.TempDir()

	// First init: write a log entry
	if err := InitWithDataDir("info", dir); err != nil {
		t.Fatal(err)
	}
	logPath := LogFilePath()
	if err := os.WriteFile(logPath, []byte("previous run\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	Close()

	// Second init: should rotate previous log to .1
	if err := InitWithDataDir("info", dir); err != nil {
		t.Fatal(err)
	}
	defer Close()

	data, err := os.ReadFile(logPath + ".1")
	if err != nil {
		t.Fatalf("roost.log.1 not found after second init: %v", err)
	}
	if string(data) != "previous run\n" {
		t.Errorf("unexpected content in .1: %q", string(data))
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

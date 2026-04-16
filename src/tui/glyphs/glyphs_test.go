package glyphs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func reset(t *testing.T) {
	t.Helper()
	userSet = nil
	Use("nerd")
}

func TestEmbeddedSetsLoad(t *testing.T) {
	if len(builtinSets["nerd"]) == 0 {
		t.Fatal("nerd set is empty")
	}
	if len(builtinSets["ascii"]) == 0 {
		t.Fatal("ascii set is empty")
	}
}

func TestDefaultIsNerd(t *testing.T) {
	reset(t)
	if Active() != "nerd" {
		t.Fatalf("want nerd, got %s", Active())
	}
}

func TestUseASCII(t *testing.T) {
	reset(t)
	Use("ascii")
	defer reset(t)

	got := Get("status.running")
	want := builtinSets["ascii"]["status.running"]
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestUnknownKeyReturnsFallback(t *testing.T) {
	reset(t)
	if got := Get("no.such.key"); got != "?" {
		t.Fatalf("want ?, got %q", got)
	}
}

func TestUserJSONPartialOverride(t *testing.T) {
	reset(t)
	defer reset(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "glyphs.json")
	f := glyphFile{
		Name:   "user",
		Glyphs: map[string]string{"status.running": "★"},
	}
	data, _ := json.Marshal(f)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Load(path); err != nil {
		t.Fatal(err)
	}

	// overridden key
	if got := Get("status.running"); got != "★" {
		t.Fatalf("want ★, got %q", got)
	}
	// non-overridden key falls back to active set
	nerdFold := builtinSets["nerd"]["fold.open"]
	if got := Get("fold.open"); got != nerdFold {
		t.Fatalf("want %q, got %q", nerdFold, got)
	}
}

func TestLoadMissingFileIsNoOp(t *testing.T) {
	reset(t)
	if err := Load("/no/such/file/glyphs.json"); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMalformedJSON(t *testing.T) {
	reset(t)
	defer reset(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "glyphs.json")
	if err := os.WriteFile(path, []byte("{bad json}"), 0o644); err != nil {
		t.Fatal(err)
	}
	// malformed JSON: no error returned, user set stays nil
	if err := Load(path); err != nil {
		t.Fatal(err)
	}
	if userSet != nil {
		t.Fatal("userSet should remain nil on parse error")
	}
}

func TestAllBuiltinKeysHaveValues(t *testing.T) {
	keys := []string{
		"status.running", "status.waiting", "status.idle", "status.stopped", "status.pending",
		"tag.branch", "workspace", "filter", "palette",
		"fold.open", "fold.closed", "tab.info", "tab.log", "link",
	}
	for _, setName := range []string{"nerd", "ascii"} {
		s := builtinSets[setName]
		for _, k := range keys {
			if s[k] == "" {
				t.Errorf("set %q: key %q is empty", setName, k)
			}
		}
	}
}

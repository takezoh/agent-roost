package github

import (
	"testing"
)

func TestParseItemsValid(t *testing.T) {
	data := []byte(`[
		{"number":1,"title":"Fix bug","repository":{"nameWithOwner":"org/repo"},"url":"https://github.com/org/repo/pull/1","updatedAt":"2025-01-01T00:00:00Z"},
		{"number":2,"title":"Add feature","repository":{"nameWithOwner":"org/other"},"url":"https://github.com/org/other/issues/2","updatedAt":"2025-01-02T00:00:00Z"}
	]`)
	items, err := parseItems(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("len = %d, want 2", len(items))
	}
	if items[0].Number != 1 || items[0].Title != "Fix bug" || items[0].Repo != "org/repo" {
		t.Errorf("item[0] = %+v", items[0])
	}
	if items[1].Number != 2 || items[1].Repo != "org/other" {
		t.Errorf("item[1] = %+v", items[1])
	}
}

func TestParseItemsEmpty(t *testing.T) {
	items, err := parseItems([]byte(`[]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("len = %d, want 0", len(items))
	}
}

func TestParseItemsInvalidJSON(t *testing.T) {
	_, err := parseItems([]byte(`not json`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

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

func TestDedupNoOverlap(t *testing.T) {
	a := []Item{{URL: "https://a/1", Title: "A1"}}
	b := []Item{{URL: "https://b/2", Title: "B2"}}
	got := dedup(a, b)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].URL != "https://a/1" || got[1].URL != "https://b/2" {
		t.Errorf("got %+v", got)
	}
}

func TestDedupFullOverlap(t *testing.T) {
	a := []Item{{URL: "https://a/1", Title: "primary"}}
	b := []Item{{URL: "https://a/1", Title: "secondary"}}
	got := dedup(a, b)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Title != "primary" {
		t.Errorf("want primary, got %s", got[0].Title)
	}
}

func TestDedupPartialOverlap(t *testing.T) {
	a := []Item{{URL: "https://a/1"}, {URL: "https://a/2"}}
	b := []Item{{URL: "https://a/2"}, {URL: "https://b/3"}}
	got := dedup(a, b)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
}

func TestDedupEmpty(t *testing.T) {
	got := dedup(nil, nil)
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
	a := []Item{{URL: "https://a/1"}}
	got = dedup(a, nil)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	got = dedup(nil, a)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
}

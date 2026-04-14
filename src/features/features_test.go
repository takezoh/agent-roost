package features

import "testing"

func TestFromConfig(t *testing.T) {
	known := []Flag{"feat-a", "feat-b"}

	tests := []struct {
		name string
		raw  map[string]bool
		want map[Flag]bool
	}{
		{
			name: "enables known flag",
			raw:  map[string]bool{"feat-a": true},
			want: map[Flag]bool{"feat-a": true},
		},
		{
			name: "ignores unknown key",
			raw:  map[string]bool{"unknown": true, "feat-b": true},
			want: map[Flag]bool{"feat-b": true},
		},
		{
			name: "nil raw returns empty set",
			raw:  nil,
			want: map[Flag]bool{},
		},
		{
			name: "false value stored",
			raw:  map[string]bool{"feat-a": false},
			want: map[Flag]bool{"feat-a": false},
		},
		{
			name: "empty raw returns empty set",
			raw:  map[string]bool{},
			want: map[Flag]bool{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FromConfig(tc.raw, known)
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d, want %d; got %v", len(got), len(tc.want), got)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("got[%q] = %v, want %v", k, got[k], v)
				}
			}
		})
	}
}

func TestSetOn(t *testing.T) {
	s := Set{"feat-a": true, "feat-b": false}

	if !s.On("feat-a") {
		t.Error("On(feat-a) = false, want true")
	}
	if s.On("feat-b") {
		t.Error("On(feat-b) = true, want false")
	}
	if s.On("feat-c") {
		t.Error("On(feat-c) = true, want false (missing key)")
	}
}

func TestSetOn_Nil(t *testing.T) {
	var s Set
	if s.On("anything") {
		t.Error("nil Set.On should always return false")
	}
}

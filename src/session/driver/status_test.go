package driver

import "testing"

func TestStatusString(t *testing.T) {
	tests := map[Status]string{
		StatusRunning: "running",
		StatusWaiting: "waiting",
		StatusIdle:    "idle",
		StatusStopped: "stopped",
		StatusPending: "pending",
	}
	for s, want := range tests {
		if got := s.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", s, got, want)
		}
	}
}

func TestParseStatus(t *testing.T) {
	cases := []struct {
		in   string
		want Status
		ok   bool
	}{
		{"running", StatusRunning, true},
		{"waiting", StatusWaiting, true},
		{"idle", StatusIdle, true},
		{"stopped", StatusStopped, true},
		{"pending", StatusPending, true},
		{"", 0, false},
		{"unknown", 0, false},
	}
	for _, tc := range cases {
		got, ok := ParseStatus(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParseStatus(%q) = (%v, %v), want (%v, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

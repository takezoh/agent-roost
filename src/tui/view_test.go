package tui

import (
	"testing"
	"time"
)

func TestFormatElapsed(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{0, "0m"},
		{30 * time.Minute, "30m"},
		{59 * time.Minute, "59m"},
		{1 * time.Hour, "1h"},
		{23 * time.Hour, "23h"},
		{24 * time.Hour, "1d"},
		{48 * time.Hour, "2d"},
	}
	for _, tt := range tests {
		if got := formatElapsed(tt.in); got != tt.want {
			t.Errorf("formatElapsed(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

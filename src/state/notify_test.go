package state

import (
	"testing"
)

func TestClassifyStatusTransition(t *testing.T) {
	tests := []struct {
		old      Status
		next     Status
		wantKind NotifyKind
		wantOk   bool
	}{
		// PendingApproval: any → Pending
		{StatusRunning, StatusPending, NotifyKindPendingApproval, true},
		{StatusWaiting, StatusPending, NotifyKindPendingApproval, true},
		{StatusIdle, StatusPending, NotifyKindPendingApproval, true},
		{StatusStopped, StatusPending, NotifyKindPendingApproval, true},

		// Done: any → Waiting (when different)
		{StatusRunning, StatusWaiting, NotifyKindDone, true},
		{StatusPending, StatusWaiting, NotifyKindDone, true},

		// Done: any → Idle (when different)
		{StatusRunning, StatusIdle, NotifyKindDone, true},
		{StatusPending, StatusIdle, NotifyKindDone, true},

		// No transition (same value)
		{StatusRunning, StatusRunning, 0, false},
		{StatusPending, StatusPending, 0, false},
		{StatusWaiting, StatusWaiting, 0, false},
		{StatusIdle, StatusIdle, 0, false},

		// Transitions that are not noteworthy
		{StatusRunning, StatusStopped, 0, false},
		{StatusWaiting, StatusRunning, 0, false},
		{StatusIdle, StatusRunning, 0, false},
		{StatusPending, StatusRunning, 0, false},

		// Waiting → Idle: done again
		{StatusWaiting, StatusIdle, NotifyKindDone, true},
	}

	for _, tt := range tests {
		gotKind, gotOk := ClassifyStatusTransition(tt.old, tt.next)
		if gotOk != tt.wantOk || gotKind != tt.wantKind {
			t.Errorf("ClassifyStatusTransition(%v, %v) = (%v, %v), want (%v, %v)",
				tt.old, tt.next, gotKind, gotOk, tt.wantKind, tt.wantOk)
		}
	}
}

func TestParseNotifyKind(t *testing.T) {
	tests := []struct {
		input    string
		wantKind NotifyKind
		wantOk   bool
	}{
		{"pending_approval", NotifyKindPendingApproval, true},
		{"done", NotifyKindDone, true},
		{"", 0, false},
		{"unknown", 0, false},
	}
	for _, tt := range tests {
		got, ok := ParseNotifyKind(tt.input)
		if ok != tt.wantOk || got != tt.wantKind {
			t.Errorf("ParseNotifyKind(%q) = (%v, %v), want (%v, %v)",
				tt.input, got, ok, tt.wantKind, tt.wantOk)
		}
	}
}

func TestNotifyKindString(t *testing.T) {
	if s := NotifyKindPendingApproval.String(); s != "pending_approval" {
		t.Errorf("got %q, want %q", s, "pending_approval")
	}
	if s := NotifyKindDone.String(); s != "done" {
		t.Errorf("got %q, want %q", s, "done")
	}
}

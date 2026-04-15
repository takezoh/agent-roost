package state

// NotifyKind is the lifecycle moment that triggered the notification.
type NotifyKind int

const (
	NotifyKindPendingApproval NotifyKind = iota + 1
	NotifyKindDone
)

func (k NotifyKind) String() string {
	switch k {
	case NotifyKindPendingApproval:
		return "pending_approval"
	case NotifyKindDone:
		return "done"
	default:
		return "unknown"
	}
}

// ParseNotifyKind converts a string label back to NotifyKind. Returns
// ok=false for unknown values.
func ParseNotifyKind(s string) (NotifyKind, bool) {
	switch s {
	case "pending_approval":
		return NotifyKindPendingApproval, true
	case "done":
		return NotifyKindDone, true
	default:
		return 0, false
	}
}

// ClassifyStatusTransition maps an old→new status pair to a NotifyKind.
// Returns (kind, true) only for transitions that should produce a
// notification; returns (0, false) for no-ops, same-value changes, or
// transitions not classified as noteworthy.
//
// Classification rules:
//   - Any → Pending   : NotifyKindPendingApproval (tool approval required)
//   - Any → Waiting or Idle (when old != new): NotifyKindDone (agent turn finished)
func ClassifyStatusTransition(old, next Status) (NotifyKind, bool) {
	if old == next {
		return 0, false
	}
	if next == StatusPending {
		return NotifyKindPendingApproval, true
	}
	if next == StatusWaiting || next == StatusIdle {
		return NotifyKindDone, true
	}
	return 0, false
}

// EffNotify asks the runtime to fire a desktop notification. All
// session-context fields are filled by the reducer (stepDriver) so
// the runtime's Notifier can match against config rules without
// reading state.
type EffNotify struct {
	SessionID SessionID
	FrameID   FrameID
	Driver    string // drv.Name()
	Command   string // FirstToken(frame.Command)
	Project   string // sess.Project
	Kind      NotifyKind
	OldStatus Status
	NewStatus Status
}

func (EffNotify) isEffect() {}

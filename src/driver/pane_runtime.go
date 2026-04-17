package driver

import (
	"strings"
	"time"

	"github.com/takezoh/agent-roost/driver/vt"
	"github.com/takezoh/agent-roost/state"
)

// PanePolling holds the capture-pane hash-based polling state shared by
// GenericDriver and ShellDriver.
type PanePolling struct {
	IdleThreshold time.Duration
	Primed        bool
	Hash          string
	LastActivity  time.Time
}

// paneTickEffects returns the branch-detect and capture-pane effects for a
// polling driver tick. Callers must handle the early return for
// parked+waiting sessions before calling this.
func paneTickEffects(common *CommonState, e state.DEvTick) []state.Effect {
	var effs []state.Effect

	if e.Active && !common.BranchInFlight {
		target := common.StartDir
		if target == "" {
			target = e.Project
		}
		if target != "" && (target != common.BranchTarget || e.Now.Sub(common.BranchAt) >= commonBranchRefreshInterval) {
			common.BranchInFlight = true
			common.BranchTarget = target
			effs = append(effs, state.EffStartJob{
				Input: BranchDetectInput{WorkingDir: target},
			})
		}
	}

	if e.PaneTarget != "" {
		effs = append(effs, state.EffStartJob{
			Input: CapturePaneInput{PaneTarget: e.PaneTarget, NLines: 30},
		})
	}

	return effs
}

// applyPollingBaseline handles priming the baseline on the first capture and
// detecting screen changes (hash differs or dirty count > 0).
//
// Returns true if the snapshot was fully handled. When false, the screen is
// stable and the caller should apply its own heuristics before returning.
func applyPollingBaseline(p *PanePolling, common *CommonState, now time.Time, snap vt.Snapshot) (handled bool) {
	if !p.Primed {
		p.Primed = true
		p.Hash = snap.Stable
		if p.LastActivity.IsZero() {
			p.LastActivity = now
		}
		return true
	}

	if snap.Stable != p.Hash || snap.DirtyCount > 0 {
		p.Hash = snap.Stable
		p.LastActivity = now
		if common.Status == state.StatusWaiting {
			common.Status = state.StatusRunning
			common.StatusChangedAt = now
		}
		return true
	}

	return false
}

// applyIdleThresholdFallback transitions Running → Waiting when the idle
// threshold has elapsed since the last screen activity.
func applyIdleThresholdFallback(p PanePolling, common *CommonState, now time.Time) {
	if common.Status == state.StatusRunning && p.IdleThreshold > 0 && now.Sub(p.LastActivity) >= p.IdleThreshold {
		common.Status = state.StatusWaiting
		common.StatusChangedAt = now
	}
}

// extractOscNotificationEffects converts OSC 9/99/777 notifications from a
// snapshot into EffRecordNotification effects.
func extractOscNotificationEffects(notifs []vt.OscNotification) []state.Effect {
	var effs []state.Effect
	for _, notif := range notifs {
		title, body := parseOscNotif(notif)
		if title == "" && body == "" {
			continue
		}
		effs = append(effs, state.EffRecordNotification{
			Cmd:   notif.Cmd,
			Title: title,
			Body:  body,
		})
	}
	return effs
}

// parseOscNotif extracts title and body from an OSC notification payload.
// OSC 9 (iTerm2): payload is the title text.
// OSC 777 (urxvt): payload is "notify;<title>;<body>".
// OSC 99 (Kitty): colon-separated key=value pairs; d= is title, p= is body.
//
//	Example: "i=1:d=My Title:p=Some body text:f=false:o=always"
func parseOscNotif(n vt.OscNotification) (title, body string) {
	switch n.Cmd {
	case 9:
		return strings.TrimSpace(n.Payload), ""
	case 777:
		parts := strings.SplitN(n.Payload, ";", 3)
		if len(parts) >= 3 {
			return parts[1], parts[2]
		}
		if len(parts) == 2 {
			return parts[1], ""
		}
	case 99:
		title, body = parseKittyPayload(n.Payload)
		if title == "" && body == "" {
			body = n.Payload
		}
	}
	return title, body
}

// parseKittyPayload decodes Kitty's OSC 99 key=value payload.
// Keys: d (title/description), p (body). Unknown keys are ignored.
func parseKittyPayload(payload string) (title, body string) {
	for _, part := range strings.Split(payload, ":") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch k {
		case "d":
			title = v
		case "p":
			body = v
		}
	}
	return title, body
}

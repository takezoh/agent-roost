package driver

import (
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

// paneTickEffects returns the branch-detect effect for a polling driver tick,
// and issues a background capture-pane only for background (non-active)
// sessions. Active sessions receive capture-pane via paneActivityEffects
// which is triggered by the PaneTap reader.
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

	// Background capture-pane for hang detection. Active sessions get
	// their capture-pane triggered by EvPaneActivity instead of the tick.
	if !e.Active && e.PaneTarget != "" && !common.CaptureInFlight {
		if common.PaneHash == "" || (e.N+e.Seq)%commonCaptureInterval == 0 {
			common.CaptureInFlight = true
			effs = append(effs, state.EffStartJob{
				Input: CapturePaneInput{PaneTarget: e.PaneTarget, NLines: 30},
			})
		}
	}

	return effs
}

// paneActivityEffects issues a capture-pane job for an active session in
// response to a DEvPaneActivity event. CaptureInFlight prevents concurrent
// in-flight captures.
func paneActivityEffects(common *CommonState, e state.DEvPaneActivity) []state.Effect {
	return common.HandleActivity(e)
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

func parseOscNotif(n vt.OscNotification) (title, body string) {
	return vt.ParseOscNotification(n)
}

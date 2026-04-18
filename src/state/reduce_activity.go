package state

// reducePaneActivity routes a PaneTap activity event to the owning frame's
// driver as DEvPaneActivity. The driver responds by issuing a CapturePaneInput
// job (guarded by CaptureInFlight) to sample the current screen state.
// Drivers that do not rely on capture-pane for status detection (e.g. Claude)
// may no-op the event.
func reducePaneActivity(s State, e EvPaneActivity) (State, []Effect) {
	s.Now = e.Now
	next, effs, _, ok := stepDriver(s, e.FrameID, DEvPaneActivity{
		PaneTarget: e.PaneTarget,
		Now:        e.Now,
	})
	if !ok {
		return s, nil
	}
	if len(effs) > 0 {
		effs = append(effs, EffPersistSnapshot{}, EffBroadcastSessionsChanged{})
	}
	return next, effs
}

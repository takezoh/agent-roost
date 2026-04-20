package state

// reducePaneOsc handles an OSC notification event fired by the PaneTap reader.
// OSC 0 (window title) is routed to the driver via DEvPaneOsc so each driver
// can interpret the title string in its own way (e.g. Claude reads Braille
// spinner vs ✳ to infer working/waiting status). OSC 9/99/777 are broadcast
// directly as EffRecordNotification.
func reducePaneOsc(s State, e EvPaneOsc) (State, []Effect) {
	if e.Cmd == 0 {
		if e.Title == "" {
			return s, nil
		}
		next, effs, _ := stepDriver(s, e.FrameID, DEvPaneOsc{Cmd: 0, Title: e.Title, Now: e.Now})
		return next, effs
	}

	if e.Title == "" && e.Body == "" {
		return s, nil
	}
	sessID, _, _, ok := findFrame(s, e.FrameID)
	if !ok {
		return s, nil
	}
	return s, []Effect{EffRecordNotification{
		SessionID: sessID,
		FrameID:   e.FrameID,
		Cmd:       e.Cmd,
		Title:     e.Title,
		Body:      e.Body,
	}}
}

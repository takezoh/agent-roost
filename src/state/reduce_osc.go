package state

// reducePaneOsc handles an OSC notification event fired by the PaneTap reader.
// It routes the parsed notification to EffRecordNotification so the runtime can
// broadcast it to TUI subscribers, append it to the EVENTS log, and dispatch a
// desktop notification.
func reducePaneOsc(s State, e EvPaneOsc) (State, []Effect) {
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

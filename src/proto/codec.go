package proto

import (
	"encoding/json"
	"fmt"
)

// EncodeCommand returns the wire bytes (NOT newline-terminated) for a
// typed command. Pair it with a json.Encoder or NDJSON writer that
// adds the trailing newline.
func EncodeCommand(reqID string, c Command) ([]byte, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("proto: marshal command data: %w", err)
	}
	env := Envelope{
		Type:  TypeCommand,
		ReqID: reqID,
		Cmd:   c.CommandName(),
		Data:  data,
	}
	return json.Marshal(env)
}

// EncodeResponse returns the wire bytes for a successful response.
func EncodeResponse(reqID string, r Response) ([]byte, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("proto: marshal response data: %w", err)
	}
	env := Envelope{
		Type:   TypeResponse,
		ReqID:  reqID,
		Status: StatusOK,
		Data:   data,
	}
	return json.Marshal(env)
}

// EncodeError returns the wire bytes for an error response.
func EncodeError(reqID string, code ErrCode, message string, details map[string]any) ([]byte, error) {
	env := Envelope{
		Type:   TypeResponse,
		ReqID:  reqID,
		Status: StatusError,
		Error: &ErrorBody{
			Code:    code,
			Message: message,
			Details: details,
		},
	}
	return json.Marshal(env)
}

// EncodeEvent returns the wire bytes for a broadcast event.
func EncodeEvent(e ServerEvent) ([]byte, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("proto: marshal event data: %w", err)
	}
	env := Envelope{
		Type: TypeEvent,
		Name: e.EventName(),
		Data: data,
	}
	return json.Marshal(env)
}

// DecodeEnvelope parses one wire frame into the bare envelope. The
// caller then dispatches into Decode{Command,Response,Event}
// depending on Envelope.Type.
func DecodeEnvelope(raw []byte) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return Envelope{}, fmt.Errorf("proto: decode envelope: %w", err)
	}
	return env, nil
}

// DecodeCommand asserts an envelope is a Command and returns the
// typed value. Returns an error for envelopes of the wrong type or
// for unknown command names.
func DecodeCommand(env Envelope) (Command, error) {
	if env.Type != TypeCommand {
		return nil, fmt.Errorf("proto: not a command envelope: type=%q", env.Type)
	}
	switch env.Cmd {
	case CmdNameSubscribe:
		var c CmdSubscribe
		return decodeInto(env.Data, &c)
	case CmdNameUnsubscribe:
		var c CmdUnsubscribe
		return decodeInto(env.Data, &c)
	case CmdNameCreateSession:
		var c CmdCreateSession
		return decodeInto(env.Data, &c)
	case CmdNameStopSession:
		var c CmdStopSession
		return decodeInto(env.Data, &c)
	case CmdNameListSessions:
		var c CmdListSessions
		return decodeInto(env.Data, &c)
	case CmdNamePreviewSession:
		var c CmdPreviewSession
		return decodeInto(env.Data, &c)
	case CmdNameSwitchSession:
		var c CmdSwitchSession
		return decodeInto(env.Data, &c)
	case CmdNamePreviewProject:
		var c CmdPreviewProject
		return decodeInto(env.Data, &c)
	case CmdNameFocusPane:
		var c CmdFocusPane
		return decodeInto(env.Data, &c)
	case CmdNameLaunchTool:
		var c CmdLaunchTool
		return decodeInto(env.Data, &c)
	case CmdNameHook:
		var c CmdHook
		return decodeInto(env.Data, &c)
	case CmdNameShutdown:
		var c CmdShutdown
		return decodeInto(env.Data, &c)
	case CmdNameDetach:
		var c CmdDetach
		return decodeInto(env.Data, &c)
	}
	return nil, fmt.Errorf("proto: unknown command: %q", env.Cmd)
}

// DecodeResponse asserts an envelope is a Response (success path).
// For error responses, callers should check Envelope.Error directly
// and skip DecodeResponse — the error body has its own typed shape.
func DecodeResponse(env Envelope, target Response) error {
	if env.Type != TypeResponse {
		return fmt.Errorf("proto: not a response envelope: type=%q", env.Type)
	}
	if env.Status != StatusOK {
		return fmt.Errorf("proto: response is not ok: status=%q", env.Status)
	}
	if len(env.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(env.Data, target); err != nil {
		return fmt.Errorf("proto: unmarshal response data: %w", err)
	}
	return nil
}

// DecodeEvent asserts an envelope is an Event and returns the typed
// value. Returns an error for envelopes of the wrong type or unknown
// event names.
func DecodeEvent(env Envelope) (ServerEvent, error) {
	if env.Type != TypeEvent {
		return nil, fmt.Errorf("proto: not an event envelope: type=%q", env.Type)
	}
	switch env.Name {
	case EvtNameSessionsChanged:
		var e EvtSessionsChanged
		if err := json.Unmarshal(env.Data, &e); err != nil {
			return nil, err
		}
		return e, nil
	case EvtNameProjectSelected:
		var e EvtProjectSelected
		if err := json.Unmarshal(env.Data, &e); err != nil {
			return nil, err
		}
		return e, nil
	case EvtNamePaneFocused:
		var e EvtPaneFocused
		if err := json.Unmarshal(env.Data, &e); err != nil {
			return nil, err
		}
		return e, nil
	case EvtNameLogLine:
		var e EvtLogLine
		if err := json.Unmarshal(env.Data, &e); err != nil {
			return nil, err
		}
		return e, nil
	case EvtNameTranscriptLine:
		var e EvtTranscriptLine
		if err := json.Unmarshal(env.Data, &e); err != nil {
			return nil, err
		}
		return e, nil
	}
	return nil, fmt.Errorf("proto: unknown event: %q", env.Name)
}

// decodeInto unmarshals raw JSON into a Command pointer and returns
// the dereferenced value as a Command interface. Used to factor out
// the boilerplate inside DecodeCommand's switch.
func decodeInto[T Command](data []byte, into *T) (Command, error) {
	if len(data) == 0 {
		return *into, nil
	}
	if err := json.Unmarshal(data, into); err != nil {
		return nil, fmt.Errorf("proto: unmarshal command data: %w", err)
	}
	return *into, nil
}

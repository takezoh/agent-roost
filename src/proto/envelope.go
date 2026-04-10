// Package proto holds the typed wire format for roost IPC. Three
// envelope kinds (cmd / resp / evt) carry typed Command, Response,
// and ServerEvent values respectively. The wire is line-delimited
// JSON; encoding/decoding is two-stage so the typed values can stay
// in their own files without one giant union.
//
// The package is dependency-free apart from state (for SessionID type
// re-use). Importing into the daemon, TUI, and hook bridge gives all
// three a typed view of the IPC contract.
package proto

import "encoding/json"

// Envelope is the outer JSON shape every wire message has. Decoders
// inspect Type to dispatch into the right typed payload.
//
// Type values:
//   - "cmd"  → Command (request from caller, awaits response)
//   - "resp" → Response (server's reply, paired with the request via ReqID)
//   - "evt"  → ServerEvent (broadcast push, no ReqID)
type Envelope struct {
	Type   string          `json:"type"`
	ReqID  string          `json:"req_id,omitempty"`
	Cmd    string          `json:"cmd,omitempty"`
	Name   string          `json:"name,omitempty"`
	Status string          `json:"status,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
	Error  *ErrorBody      `json:"error,omitempty"`
}

// Envelope type discriminator constants. Stringly-typed in JSON for
// interoperability with non-Go clients (although roost only ships its
// own client today).
const (
	TypeCommand  = "cmd"
	TypeResponse = "resp"
	TypeEvent    = "evt"
)

// Status values for response envelopes.
const (
	StatusOK    = "ok"
	StatusError = "error"
)

// ErrorBody is the structured error payload that rides inside a
// "resp" envelope when Status == "error". Code is a typed enum (see
// errors.go); Message is human-readable; Details is an open map for
// caller-actionable hints (e.g. {"session_id": "..."}).
type ErrorBody struct {
	Code    ErrCode        `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

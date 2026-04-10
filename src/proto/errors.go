package proto

// ErrCode is the typed enum carried inside ErrorBody. The set is
// closed: callers can switch on it without a default case (the
// compiler doesn't enforce exhaustiveness, but linters can).
type ErrCode string

const (
	ErrUnknown         ErrCode = "unknown"
	ErrNotFound        ErrCode = "not_found"
	ErrInvalidArgument ErrCode = "invalid_argument"
	ErrInternal        ErrCode = "internal"
	ErrSessionStopped  ErrCode = "session_stopped"
	ErrAlreadyExists   ErrCode = "already_exists"
	ErrUnsupported     ErrCode = "unsupported"
)

// FromStateCode translates a state-package error code string into a
// typed proto.ErrCode. Unknown codes map to ErrUnknown so the wire
// always carries a recognized value.
func FromStateCode(code string) ErrCode {
	switch code {
	case "not_found":
		return ErrNotFound
	case "invalid_argument":
		return ErrInvalidArgument
	case "internal":
		return ErrInternal
	case "session_stopped":
		return ErrSessionStopped
	case "already_exists":
		return ErrAlreadyExists
	case "unsupported":
		return ErrUnsupported
	}
	return ErrUnknown
}

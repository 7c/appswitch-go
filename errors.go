package appswitch

import "errors"

// Code is a machine-readable error code: the openapi vocabulary the server
// returns, plus client-side codes (CLIENT.md §10).
type Code string

const (
	// server-originated
	CodeNotFound          Code = "NOT_FOUND"
	CodeUnauthorized      Code = "UNAUTHORIZED"
	CodeForbidden         Code = "FORBIDDEN"
	CodeLinkCycle         Code = "LINK_CYCLE"
	CodeLinkTargetMissing Code = "LINK_TARGET_MISSING"
	CodeTooManyRequests   Code = "TOO_MANY_REQUESTS"
	CodeLocked            Code = "LOCKED"
	// client-side
	CodeNotReady     Code = "NOT_READY"
	CodeStale        Code = "STALE"
	CodeTypeMismatch Code = "TYPE_MISMATCH"
	CodeNetwork      Code = "NETWORK"
	CodeTimeout      Code = "TIMEOUT"
	CodeConfig       Code = "CONFIG"
)

// Error is the SDK's error type. Callers branch on Code via errors.As.
type Error struct {
	Code    Code
	Message string
	Err     error // wrapped cause, if any
}

func (e *Error) Error() string {
	if e.Err != nil {
		return string(e.Code) + ": " + e.Message + ": " + e.Err.Error()
	}
	return string(e.Code) + ": " + e.Message
}

func (e *Error) Unwrap() error { return e.Err }

func newError(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

func wrapError(code Code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, Err: cause}
}

// availabilityCodes mean "value unavailable" — eligible for a per-call fallback.
var availabilityCodes = map[Code]bool{
	CodeNotReady: true,
	CodeNotFound: true,
	CodeStale:    true,
	CodeNetwork:  true,
	CodeTimeout:  true,
}

func isAvailabilityErr(err error) bool {
	var ae *Error
	if errors.As(err, &ae) {
		return availabilityCodes[ae.Code]
	}
	return false
}

// CodeOf extracts the Code from an error, or "" if it isn't an *Error.
func CodeOf(err error) Code {
	var ae *Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

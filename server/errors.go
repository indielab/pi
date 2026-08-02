package server

import (
	"slices"

	"github.com/sky-valley/pi/protocol"
)

// Default messages for the three named failures pi exports as Error subclasses.
const (
	defaultBusyMessage     = "Session is busy"
	defaultLockedMessage   = "Session is locked"
	defaultNotFoundMessage = "Session was not found"
)

// operationErrorCodes are the protocol error codes a backend or runtime is
// allowed to put on the wire. auth and version belong to the handshake and are
// the server's alone to report.
var operationErrorCodes = []protocol.ProtocolErrorCode{
	protocol.ErrorBusy,
	protocol.ErrorSessionLocked,
	protocol.ErrorNotFound,
	protocol.ErrorInvalidRequest,
}

// Error is a backend or runtime failure that can safely cross the protocol
// boundary: its code and message reach the client verbatim. Any other error a
// backend returns is reported to the server's error observer and answered with
// a generic invalid_request, so private detail never leaks to a peer.
//
// DIVERGENCE (deliberate): pi exports SessionBusyError, SessionLockedError and
// SessionNotFoundError as subclasses that differ only in their default message.
// Go has no subclassing worth imitating here, and a caller branches on the code
// rather than the type — so this is one type plus three constructors, and
// errors.As(err, &serverErr) followed by a check on Code replaces instanceof.
type Error struct {
	Code    protocol.ProtocolErrorCode
	Message string
	Details any
}

func (e *Error) Error() string { return e.Message }

// NewError builds an Error. details may be nil.
func NewError(code protocol.ProtocolErrorCode, message string, details any) *Error {
	return &Error{Code: code, Message: message, Details: details}
}

// NewBusyError reports that a conflicting operation is already running. An
// empty message takes pi's default.
func NewBusyError(message string, details any) *Error {
	return NewError(protocol.ErrorBusy, orDefault(message, defaultBusyMessage), details)
}

// NewLockedError reports that another holder owns the session. An empty message
// takes pi's default.
func NewLockedError(message string, details any) *Error {
	return NewError(protocol.ErrorSessionLocked, orDefault(message, defaultLockedMessage), details)
}

// NewNotFoundError reports that the session does not exist. An empty message
// takes pi's default.
func NewNotFoundError(message string, details any) *Error {
	return NewError(protocol.ErrorNotFound, orDefault(message, defaultNotFoundMessage), details)
}

func orDefault(message, fallback string) string {
	if message == "" {
		return fallback
	}
	return message
}

// crossesProtocolBoundary reports whether this Error's code is one a backend
// may report.
//
// DIVERGENCE (deliberate): pi constrains the code with a TypeScript type, which
// costs nothing at runtime and cannot be violated. Go cannot express that, so
// the check moves to the one place it matters — the wire — and an out-of-range
// code is downgraded to the generic internal error rather than letting a
// backend forge an auth or version failure.
func (e *Error) crossesProtocolBoundary() bool {
	return slices.Contains(operationErrorCodes, e.Code)
}

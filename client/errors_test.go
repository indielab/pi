package client

import (
	"errors"
	"fmt"
	"testing"

	"github.com/sky-valley/pi/protocol"
)

func TestServerErrorCarriesProtocolError(t *testing.T) {
	details := map[string]any{"supported": []any{int64(2)}}
	err := newServerError(&protocol.ProtocolError{
		Code:    protocol.ErrorVersion,
		Message: "unsupported version",
		Details: details,
	})
	if err == nil {
		t.Fatal("newServerError returned nil")
	}
	if err.Error() != "unsupported version" {
		t.Errorf("Error() = %q, want %q", err.Error(), "unsupported version")
	}
	if err.Code != protocol.ErrorVersion {
		t.Errorf("Code = %q, want %q", err.Code, protocol.ErrorVersion)
	}
	if got, ok := err.Details.(map[string]any); !ok || len(got) != 1 {
		t.Errorf("Details = %#v, want the protocol error's details", err.Details)
	}
}

// TestServerErrorIsMatchableByAs is the reason these are types rather than
// fmt.Errorf strings: a caller must be able to branch on the code.
func TestServerErrorIsMatchableByAs(t *testing.T) {
	var wrapped error = fmt.Errorf("request failed: %w", newServerError(&protocol.ProtocolError{
		Code:    protocol.ErrorNotFound,
		Message: "no such session",
	}))
	var target *ServerError
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As did not find a *ServerError through a wrap")
	}
	if target.Code != protocol.ErrorNotFound {
		t.Errorf("Code = %q, want %q", target.Code, protocol.ErrorNotFound)
	}
}

// TestErrorMessages covers the message text of every client error type in one
// place; the types that also carry structured fields are asserted separately
// below.
func TestErrorMessages(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"disconnected default", &DisconnectedError{}, "Pi client is disconnected"},
		{"disconnected custom", &DisconnectedError{Message: "socket closed"}, "socket closed"},
		{"disposed", &DisposedError{}, "Pi client is disposed"},
		{"ownership", &SessionOwnershipError{SessionID: "s1", Message: "owned by another client"},
			"owned by another client"},
		{"detached", &SessionDetachedError{SessionID: "s1"}, "Session s1 is not attached"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSessionErrorsCarrySessionID(t *testing.T) {
	ownership := &SessionOwnershipError{SessionID: "s1", Message: "owned by another client"}
	if ownership.SessionID != "s1" {
		t.Errorf("SessionOwnershipError.SessionID = %q, want %q", ownership.SessionID, "s1")
	}
	detached := &SessionDetachedError{SessionID: "s2"}
	if detached.SessionID != "s2" {
		t.Errorf("SessionDetachedError.SessionID = %q, want %q", detached.SessionID, "s2")
	}
}

// TestAsDisconnectedErrorPreservesExisting locks pi's toDisconnectedError
// behaviour: an error that is already a disconnect is returned unchanged rather
// than re-wrapped, so the original message survives every hop.
func TestAsDisconnectedErrorPreservesExisting(t *testing.T) {
	original := &DisconnectedError{Message: "socket closed"}
	got := asDisconnectedError(original)
	if got != original {
		t.Errorf("asDisconnectedError returned %#v, want the original instance", got)
	}
}

func TestAsDisconnectedErrorWrapsOtherErrors(t *testing.T) {
	got := asDisconnectedError(errors.New("broken pipe"))
	if got == nil {
		t.Fatal("asDisconnectedError returned nil")
	}
	if got.Error() != "broken pipe" {
		t.Errorf("Error() = %q, want the cause's message %q", got.Error(), "broken pipe")
	}
}

// TestAsDisconnectedErrorHandlesNil: a nil cause still has to produce a usable
// disconnect, because the transport can close with no error at all.
func TestAsDisconnectedErrorHandlesNil(t *testing.T) {
	got := asDisconnectedError(nil)
	if got == nil {
		t.Fatal("asDisconnectedError(nil) returned nil")
	}
	if got.Error() != "Pi client is disconnected" {
		t.Errorf("Error() = %q, want the default message", got.Error())
	}
}

// TestAsDisconnectedErrorFindsWrapped is the Go-specific half of the
// preserve-existing rule: errors get wrapped on the way up, and a wrapped
// disconnect must still be recognised rather than flattened into a new one.
func TestAsDisconnectedErrorFindsWrapped(t *testing.T) {
	original := &DisconnectedError{Message: "socket closed"}
	got := asDisconnectedError(fmt.Errorf("send failed: %w", original))
	if got != original {
		t.Errorf("asDisconnectedError returned %#v (%q), want the wrapped original", got, got.Error())
	}
}

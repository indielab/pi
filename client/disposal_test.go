package client

import (
	"errors"
	"testing"

	"github.com/sky-valley/pi/protocol"
)

// TestConnectHelperBuildsAConnectedClient locks the one-call entry point.
func TestConnectHelperBuildsAConnectedClient(t *testing.T) {
	server := newMemoryServer(t)
	server.answerHandshake(t, baseSnapshot(1))

	client, err := Connect(testContext(t), Options{Token: "secret", TransportFactory: server.connect})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if !client.Connected() {
		t.Error("Connect returned a client that is not connected")
	}
	if snapshot := client.Snapshot(); snapshot == nil || snapshot.Revision != 1 {
		t.Errorf("Snapshot() = %#v, want the handshake snapshot", snapshot)
	}
}

// TestConnectHelperDisposesOnHandshakeFailure: a caller that never received a
// client cannot be expected to clean one up.
func TestConnectHelperDisposesOnHandshakeFailure(t *testing.T) {
	server := newMemoryServer(t)
	server.onMessage(func(protocol.ClientMessage) {
		server.send(t, &protocol.ServerHelloError{
			Type:  "hello_error",
			Error: protocol.ProtocolError{Code: protocol.ErrorAuth, Message: "Invalid token"},
		})
	})

	client, err := Connect(testContext(t), Options{Token: "wrong", TransportFactory: server.connect})
	if client != nil {
		t.Error("Connect returned a client alongside its error")
	}
	var serverErr *ServerError
	if !errors.As(err, &serverErr) {
		t.Fatalf("error = %#v, want *ServerError", err)
	}
}

// TestCloseDisconnectsInvalidatesAndRejects covers everything disposal owes its
// caller in one place: it is idempotent, it drops the connection, it retires
// child handles, and nothing is left waiting.
func TestCloseDisconnectsInvalidatesAndRejects(t *testing.T) {
	server := newMemoryServer(t)
	client := connectTestClient(t, server)
	handle := attachTestSession(t, client, server, sessionSnapshot("session-1", 1, true))
	requests := recordRequests(server)

	listErr := make(chan error, 1)
	go func() {
		_, err := client.ListSessions(testContext(t))
		listErr <- err
	}()
	requests.await(t, 1)

	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	if !client.Disposed() {
		t.Error("Disposed() is false after Close")
	}
	if client.Connected() {
		t.Error("Connected() is true after Close")
	}
	if client.ConnectionState() != Disconnected {
		t.Errorf("ConnectionState() = %q, want %q", client.ConnectionState(), Disconnected)
	}
	if handle.Attached() {
		t.Error("a child handle survived disposal")
	}

	var disposed *DisposedError
	if err := <-listErr; !errors.As(err, &disposed) {
		t.Fatalf("pending request error = %#v, want *DisposedError", err)
	}
	if _, err := handle.Prompt(testContext(t), "after disposal"); !errors.As(err, &disposed) {
		t.Fatalf("Prompt after disposal = %#v, want *DisposedError", err)
	}
}

// TestSubscriptionsAreRefusedAfterClose: a disposed client has dropped its
// listeners, so accepting a new one would register something that never fires.
func TestSubscriptionsAreRefusedAfterClose(t *testing.T) {
	server := newMemoryServer(t)
	client := connectTestClient(t, server)
	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var disposed *DisposedError
	if _, err := client.Subscribe(func(*protocol.ServerSnapshot) {}); !errors.As(err, &disposed) {
		t.Errorf("Subscribe = %#v, want *DisposedError", err)
	}
	if _, err := client.OnEvent(func(protocol.ServerEvent) {}); !errors.As(err, &disposed) {
		t.Errorf("OnEvent = %#v, want *DisposedError", err)
	}
	if _, err := client.OnConnectionStateChange(func(ConnectionStateChange) {}); !errors.As(err, &disposed) {
		t.Errorf("OnConnectionStateChange = %#v, want *DisposedError", err)
	}
	if _, err := client.Connect(testContext(t)); !errors.As(err, &disposed) {
		t.Errorf("Connect = %#v, want *DisposedError", err)
	}
	if _, err := client.AcquireSession(testContext(t), "session-1", LeaseShared); !errors.As(err, &disposed) {
		t.Errorf("AcquireSession = %#v, want *DisposedError", err)
	}
}

// TestCloseNotifiesConnectionStateListeners: subscribers are cleared last, so
// the disconnect disposal caused is still delivered.
func TestCloseNotifiesConnectionStateListeners(t *testing.T) {
	server := newMemoryServer(t)
	client := connectTestClient(t, server)
	var changes []ConnectionStateChange
	if _, err := client.OnConnectionStateChange(func(change ConnectionStateChange) {
		changes = append(changes, change)
	}); err != nil {
		t.Fatalf("OnConnectionStateChange: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if len(changes) != 1 || changes[0].State != Disconnected {
		t.Fatalf("changes = %#v, want a single disconnect", changes)
	}
	var disposed *DisposedError
	if !errors.As(changes[0].Err, &disposed) {
		t.Errorf("disconnect cause = %#v, want *DisposedError", changes[0].Err)
	}
}

// TestConnectionStateUnsubscribeIsIdempotent: callers hold these and run them
// from a defer as well as explicitly.
func TestConnectionStateUnsubscribeIsIdempotent(t *testing.T) {
	server := newMemoryServer(t)
	client := connectTestClient(t, server)
	calls := 0
	unsubscribe, err := client.OnConnectionStateChange(func(ConnectionStateChange) { calls++ })
	if err != nil {
		t.Fatalf("OnConnectionStateChange: %v", err)
	}
	unsubscribe()
	unsubscribe()

	client.Disconnect("")
	if calls != 0 {
		t.Errorf("listener ran %d times after unsubscribing", calls)
	}
}

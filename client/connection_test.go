package client

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sky-valley/pi/protocol"
	"github.com/sky-valley/pi/protocol/cbor"
)

// TestConnectSendsHelloAndAcceptsFragmentedServerHello locks the handshake: the
// client's first frame is a versioned hello carrying the bearer token, and the
// server's answer is accepted even when it arrives split across chunks.
func TestConnectSendsHelloAndAcceptsFragmentedServerHello(t *testing.T) {
	server := newMemoryServer(t)
	var received []protocol.ClientMessage
	snapshot := baseSnapshot(1)
	server.onMessage(func(message protocol.ClientMessage) {
		received = append(received, message)
		if _, ok := message.(*protocol.ClientHello); ok {
			server.sendSplit(t, serverHello(snapshot), 3)
		}
	})
	client := newTestClient(t, server)

	got, err := client.Connect(testContext(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if got.Revision != snapshot.Revision || got.ServerID != snapshot.ServerID {
		t.Errorf("Connect returned %#v, want the server's snapshot", got)
	}
	if len(received) != 1 {
		t.Fatalf("server saw %d messages, want 1", len(received))
	}
	hello, ok := received[0].(*protocol.ClientHello)
	if !ok {
		t.Fatalf("first client message was %T, want *protocol.ClientHello", received[0])
	}
	if hello.Version != protocol.ProtocolVersion || hello.Token != "bearer-secret" {
		t.Errorf("hello = %#v, want version %d and the configured token", hello, protocol.ProtocolVersion)
	}
	if client.ConnectionState() != Connected {
		t.Errorf("ConnectionState() = %q, want %q", client.ConnectionState(), Connected)
	}
}

// TestConnectRejectsServerDataBeforeClientHello covers a transport that hands
// back bytes before it has even returned: nothing may be sent, the transport is
// closed exactly once, and the handshake fails as a protocol violation.
func TestConnectRejectsServerDataBeforeClientHello(t *testing.T) {
	var mu sync.Mutex
	sendCount, closeCount := 0, 0
	client, err := New(Options{
		Token: "bearer-secret",
		TransportFactory: func(handlers TransportHandlers) (Transport, error) {
			handlers.OnData(encodeServer(t, serverHello(baseSnapshot(1))))
			return &countingTransport{mu: &mu, sends: &sendCount, closes: &closeCount}, nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	_, connectErr := client.Connect(testContext(t))
	var validation *protocol.ValidationError
	if !errors.As(connectErr, &validation) {
		t.Fatalf("Connect error = %#v, want *protocol.ValidationError", connectErr)
	}
	if validation.Msg != "Received server data before the client hello was sent" {
		t.Errorf("message = %q, want pi's wording", validation.Msg)
	}
	if client.ConnectionState() != Disconnected {
		t.Errorf("ConnectionState() = %q, want %q", client.ConnectionState(), Disconnected)
	}
	mu.Lock()
	defer mu.Unlock()
	if sendCount != 0 {
		t.Errorf("sendCount = %d, want 0", sendCount)
	}
	if closeCount != 1 {
		t.Errorf("closeCount = %d, want 1", closeCount)
	}
}

type countingTransport struct {
	mu     *sync.Mutex
	sends  *int
	closes *int
}

func (c *countingTransport) Send([]byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	*c.sends++
	return nil
}

func (c *countingTransport) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	*c.closes++
	return nil
}

// TestSubscriberPanicDoesNotBreakHandshake locks pi's isolation guarantee: a
// broken subscriber is contained and reported, and the connection survives it.
func TestSubscriberPanicDoesNotBreakHandshake(t *testing.T) {
	for _, tc := range []struct {
		name    string
		observe bool
	}{
		{"without a listener-error handler", false},
		{"with a listener-error handler", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := newMemoryServer(t)
			var mu sync.Mutex
			var listenerErrors []error
			opts := Options{Token: "bearer-secret", TransportFactory: server.connect}
			if tc.observe {
				opts.OnListenerError = func(err error) {
					mu.Lock()
					defer mu.Unlock()
					listenerErrors = append(listenerErrors, err)
				}
			}
			client, err := New(opts)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = client.Close() })
			server.answerHandshake(t, baseSnapshot(1))
			if _, err := client.Subscribe(func(*protocol.ServerSnapshot) {
				panic(errors.New("consumer failure"))
			}); err != nil {
				t.Fatalf("Subscribe: %v", err)
			}

			if _, err := client.Connect(testContext(t)); err != nil {
				t.Fatalf("Connect: %v", err)
			}
			if client.ConnectionState() != Connected {
				t.Errorf("ConnectionState() = %q, want %q", client.ConnectionState(), Connected)
			}
			mu.Lock()
			defer mu.Unlock()
			if tc.observe {
				if len(listenerErrors) != 1 || listenerErrors[0].Error() != "consumer failure" {
					t.Errorf("listener errors = %v, want one \"consumer failure\"", listenerErrors)
				}
			} else if len(listenerErrors) != 0 {
				t.Errorf("listener errors = %v, want none", listenerErrors)
			}
		})
	}
}

// TestHandshakeIsNotRestoredAfterListenerDisconnects: a snapshot listener that
// disconnects mid-handshake must win. Reporting "connected" afterwards would
// hand the caller a connection that no longer exists.
func TestHandshakeIsNotRestoredAfterListenerDisconnects(t *testing.T) {
	server := newMemoryServer(t)
	client := newTestClient(t, server)
	server.answerHandshake(t, baseSnapshot(1))
	if _, err := client.Subscribe(func(*protocol.ServerSnapshot) { client.Disconnect("") }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	_, err := client.Connect(testContext(t))
	var disconnected *DisconnectedError
	if !errors.As(err, &disconnected) {
		t.Fatalf("Connect error = %#v, want *DisconnectedError", err)
	}
	if client.ConnectionState() != Disconnected {
		t.Errorf("ConnectionState() = %q, want %q", client.ConnectionState(), Disconnected)
	}
	if got := server.clientCloseCount(); got != 1 {
		t.Errorf("transport closes = %d, want 1", got)
	}
}

// TestStaleHandshakeIsNotRestoredWhenListenerReconnects: the listener replaces
// the connection outright. The superseded attempt must not report success, and
// the new one must survive.
func TestStaleHandshakeIsNotRestoredWhenListenerReconnects(t *testing.T) {
	first, second := newMemoryServer(t), newMemoryServer(t)
	first.answerHandshake(t, baseSnapshot(1))
	second.answerHandshake(t, baseSnapshot(2))

	var mu sync.Mutex
	attempts := 0
	client, err := New(Options{
		Token: "bearer-secret",
		TransportFactory: func(handlers TransportHandlers) (Transport, error) {
			mu.Lock()
			attempt := attempts
			attempts++
			mu.Unlock()
			if attempt == 0 {
				return first.connect(handlers)
			}
			return second.connect(handlers)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	var reconnected *protocol.ServerSnapshot
	var reconnectErr error
	requested := false
	if _, err := client.Subscribe(func(*protocol.ServerSnapshot) {
		if requested {
			return
		}
		requested = true
		client.Disconnect("")
		reconnected, reconnectErr = client.Reconnect(testContext(t))
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	_, err = client.Connect(testContext(t))
	var disconnected *DisconnectedError
	if !errors.As(err, &disconnected) {
		t.Fatalf("Connect error = %#v, want *DisconnectedError", err)
	}
	if reconnectErr != nil {
		t.Fatalf("Reconnect: %v", reconnectErr)
	}
	if reconnected == nil || reconnected.Revision != 2 {
		t.Errorf("Reconnect snapshot = %#v, want revision 2", reconnected)
	}
	if client.ConnectionState() != Connected {
		t.Errorf("ConnectionState() = %q, want %q", client.ConnectionState(), Connected)
	}
	if got := first.clientCloseCount(); got != 1 {
		t.Errorf("first transport closes = %d, want 1", got)
	}
}

// TestConnectRejectsHandshakeError locks the typed rejection: the server's
// error code has to survive as something a caller can branch on.
func TestConnectRejectsHandshakeError(t *testing.T) {
	server := newMemoryServer(t)
	server.onMessage(func(protocol.ClientMessage) {
		server.send(t, &protocol.ServerHelloError{
			Type:  "hello_error",
			Error: protocol.ProtocolError{Code: protocol.ErrorAuth, Message: "Invalid token"},
		})
	})
	client := newTestClient(t, server)

	_, err := client.Connect(testContext(t))
	var serverErr *ServerError
	if !errors.As(err, &serverErr) {
		t.Fatalf("Connect error = %#v, want *ServerError", err)
	}
	if serverErr.Code != protocol.ErrorAuth || serverErr.Message != "Invalid token" {
		t.Errorf("server error = %#v, want the auth error the server sent", serverErr)
	}
	if client.ConnectionState() != Disconnected {
		t.Errorf("ConnectionState() = %q, want %q", client.ConnectionState(), Disconnected)
	}
	if got := server.clientCloseCount(); got != 1 {
		t.Errorf("transport closes = %d, want 1", got)
	}
}

// TestCloseRejectsPendingRequestsAndAllowsReconnect covers the full lifecycle
// sequence a consumer observes across a drop and a reconnect through a fresh
// transport.
func TestCloseRejectsPendingRequestsAndAllowsReconnect(t *testing.T) {
	first, second := newMemoryServer(t), newMemoryServer(t)
	first.answerHandshake(t, baseSnapshot(1))
	second.answerHandshake(t, baseSnapshot(2))
	var mu sync.Mutex
	attempts := 0
	client, err := New(Options{
		Token: "bearer-secret",
		TransportFactory: func(handlers TransportHandlers) (Transport, error) {
			mu.Lock()
			attempt := attempts
			attempts++
			mu.Unlock()
			if attempt == 0 {
				return first.connect(handlers)
			}
			return second.connect(handlers)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	var states []ConnectionState
	if _, err := client.OnConnectionStateChange(func(change ConnectionStateChange) {
		mu.Lock()
		defer mu.Unlock()
		states = append(states, change.State)
	}); err != nil {
		t.Fatalf("OnConnectionStateChange: %v", err)
	}
	if _, err := client.Connect(testContext(t)); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	requests := recordRequests(first)
	listErr := make(chan error, 1)
	go func() {
		_, err := client.ListSessions(testContext(t))
		listErr <- err
	}()
	requests.await(t, 1)
	first.closeTransport()

	var disconnected *DisconnectedError
	if err := <-listErr; !errors.As(err, &disconnected) {
		t.Fatalf("pending request error = %#v, want *DisconnectedError", err)
	}
	if client.ConnectionState() != Disconnected {
		t.Errorf("ConnectionState() = %q, want %q", client.ConnectionState(), Disconnected)
	}

	snapshot, err := client.Reconnect(testContext(t))
	if err != nil {
		t.Fatalf("Reconnect: %v", err)
	}
	if snapshot.Revision != 2 {
		t.Errorf("Reconnect snapshot revision = %d, want 2", snapshot.Revision)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []ConnectionState{Connecting, Connected, Disconnected, Connecting, Connected}
	if len(states) != len(want) {
		t.Fatalf("states = %v, want %v", states, want)
	}
	for i := range want {
		if states[i] != want[i] {
			t.Fatalf("states = %v, want %v", states, want)
		}
	}
}

// TestReconnectFromDisconnectionListener: a listener is allowed to bring the
// connection straight back up, which is how a supervisor is written.
func TestReconnectFromDisconnectionListener(t *testing.T) {
	first, second := newMemoryServer(t), newMemoryServer(t)
	first.answerHandshake(t, baseSnapshot(1))
	second.answerHandshake(t, baseSnapshot(2))
	var mu sync.Mutex
	attempts := 0
	client, err := New(Options{
		Token: "bearer-secret",
		TransportFactory: func(handlers TransportHandlers) (Transport, error) {
			mu.Lock()
			attempt := attempts
			attempts++
			mu.Unlock()
			if attempt == 0 {
				return first.connect(handlers)
			}
			return second.connect(handlers)
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if _, err := client.Connect(testContext(t)); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	var reconnected *protocol.ServerSnapshot
	var reconnectErr error
	if _, err := client.OnConnectionStateChange(func(change ConnectionStateChange) {
		if change.State == Disconnected && reconnected == nil && reconnectErr == nil {
			reconnected, reconnectErr = client.Reconnect(testContext(t))
		}
	}); err != nil {
		t.Fatalf("OnConnectionStateChange: %v", err)
	}

	first.closeTransport()

	if reconnectErr != nil {
		t.Fatalf("Reconnect: %v", reconnectErr)
	}
	if reconnected == nil || reconnected.Revision != 2 {
		t.Errorf("Reconnect snapshot = %#v, want revision 2", reconnected)
	}
	if client.ConnectionState() != Connected {
		t.Errorf("ConnectionState() = %q, want %q", client.ConnectionState(), Connected)
	}
}

// TestTransportErrorRejectsPendingRequests locks that the transport's own
// message survives onto the disconnect the caller sees.
func TestTransportErrorRejectsPendingRequests(t *testing.T) {
	server := newMemoryServer(t)
	client := connectTestClient(t, server)
	requests := recordRequests(server)
	listErr := make(chan error, 1)
	go func() {
		_, err := client.ListSessions(testContext(t))
		listErr <- err
	}()
	requests.await(t, 1)
	server.fail(errors.New("read failed"))

	err := <-listErr
	var disconnected *DisconnectedError
	if !errors.As(err, &disconnected) {
		t.Fatalf("error = %#v, want *DisconnectedError", err)
	}
	if disconnected.Error() != "read failed" {
		t.Errorf("message = %q, want the transport's message", disconnected.Error())
	}
	if client.ConnectionState() != Disconnected {
		t.Errorf("ConnectionState() = %q, want %q", client.ConnectionState(), Disconnected)
	}
}

// TestFrameLimitAppliesBothDirections: the configured limit has to bound what
// is sent as well as what is accepted, or a peer could pick the limit for us.
func TestFrameLimitAppliesBothDirections(t *testing.T) {
	server := newMemoryServer(t)
	limit := 512
	client, err := New(Options{
		Token:            "bearer-secret",
		MaxFrameLength:   &limit,
		TransportFactory: server.connect,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	server.answerHandshake(t, baseSnapshot(1))
	if _, err := client.Connect(testContext(t)); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	handle := attachTestSession(t, client, server, sessionSnapshot("session-1", 1, true))

	sentBefore := server.sentCount()
	_, err = handle.Prompt(testContext(t), strings.Repeat("x", 1000))
	var validation *protocol.ValidationError
	var frameErr *protocol.FrameError
	if !errors.As(err, &validation) && !errors.As(err, &frameErr) {
		t.Fatalf("Prompt error = %#v, want a protocol framing/validation error", err)
	}
	if got := server.sentCount(); got != sentBefore {
		t.Errorf("client sent %d chunks after the over-long prompt, want %d", got, sentBefore)
	}

	// An inbound frame that declares a length over the limit is fatal too.
	server.sendRaw([]byte{0, 0, 2, 1})
	if client.ConnectionState() != Disconnected {
		t.Errorf("ConnectionState() = %q, want %q", client.ConnectionState(), Disconnected)
	}
}

// TestInvalidProtocolDataDisconnects: a structurally decodable frame carrying
// the wrong types is still a protocol violation, and the stream position after
// one is not trustworthy.
func TestInvalidProtocolDataDisconnects(t *testing.T) {
	server := newMemoryServer(t)
	client := connectTestClient(t, server)

	payload, err := cbor.Encode(map[string]any{
		"type":  "event",
		"event": map[string]any{"type": "session_removed", "sessionId": int64(1)},
	}, nil)
	if err != nil {
		t.Fatalf("cbor.Encode: %v", err)
	}
	frame, err := protocol.EncodeFrame(payload)
	if err != nil {
		t.Fatalf("EncodeFrame: %v", err)
	}
	server.sendRaw(frame)

	if client.ConnectionState() != Disconnected {
		t.Errorf("ConnectionState() = %q, want %q", client.ConnectionState(), Disconnected)
	}
}

// TestTruncatedFrameOnCloseIsReported: a stream that stops mid-frame is a
// protocol error, not a clean close, and the pending request must say so.
func TestTruncatedFrameOnCloseIsReported(t *testing.T) {
	server := newMemoryServer(t)
	client := connectTestClient(t, server)
	requests := recordRequests(server)
	listErr := make(chan error, 1)
	go func() {
		_, err := client.ListSessions(testContext(t))
		listErr <- err
	}()
	requests.await(t, 1)
	server.sendRaw([]byte{0, 0, 0, 2, 1})
	server.closeTransport()

	err := <-listErr
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "truncated") {
		t.Fatalf("error = %#v, want a truncated-frame report", err)
	}
	if client.ConnectionState() != Disconnected {
		t.Errorf("ConnectionState() = %q, want %q", client.ConnectionState(), Disconnected)
	}
}

// TestNewRejectsOutOfRangeFrameLimits locks the constructor's bounds. pi throws
// a TypeError here; a Go caller gets an error instead of an unusable client.
func TestNewRejectsOutOfRangeFrameLimits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit int
	}{
		{"zero", 0},
		{"negative", -1},
		{"above the 32-bit prefix", maxUint32 + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := newMemoryServer(t)
			limit := tc.limit
			_, err := New(Options{
				Token:            "secret",
				MaxFrameLength:   &limit,
				TransportFactory: server.connect,
			})
			if err == nil || !strings.Contains(err.Error(), "maxFrameLength") {
				t.Fatalf("New error = %v, want a maxFrameLength complaint", err)
			}
		})
	}
}

// TestConnectRejectsWhenNotIdle locks the guard against two overlapping
// attempts on one connection.
func TestConnectRejectsWhenNotIdle(t *testing.T) {
	server := newMemoryServer(t)
	client := connectTestClient(t, server)

	_, err := client.Connect(testContext(t))
	var disconnected *DisconnectedError
	if !errors.As(err, &disconnected) {
		t.Fatalf("Connect error = %#v, want *DisconnectedError", err)
	}
	if disconnected.Error() != "PiClient is already connected" {
		t.Errorf("message = %q, want pi's wording", disconnected.Error())
	}
}

// TestConnectHonoursContextCancellation is Go-specific: pi has no cancellation,
// so a handshake that never answers hangs forever there. Here it must not.
func TestConnectHonoursContextCancellation(t *testing.T) {
	server := newMemoryServer(t)
	// No handshake answer is registered, so the server never replies.
	client := newTestClient(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	if _, err := client.Connect(ctx); err == nil {
		t.Fatal("Connect returned no error for a cancelled context")
	}
	if client.ConnectionState() != Disconnected {
		t.Errorf("ConnectionState() = %q, want %q", client.ConnectionState(), Disconnected)
	}
}

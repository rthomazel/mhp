package transport

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/rthomazel/mhp/internal/config"
)

// TestConnectorPublishesAndWindsDown stands up a relay, drives it through the
// connector, and asserts the connector publishes the linked session and returns
// the parent's error once the context is cancelled.
func TestConnectorPublishesAndWindsDown(t *testing.T) {
	addr, pool, stop := newRelay(t, config.ModeExit, "tok")
	defer stop()

	auth := Authenticator{
		Addr: addr, Role: config.ModeExit, Token: "tok",
		Timing: fastTiming(), ServerName: "localhost", RootCAs: pool,
		MinTLSVersion: DefaultMinTLSVersion,
	}
	conn := NewConnector(auth, quietLogger(), &net.Dialer{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- conn.Run(ctx) }()

	// Poll until the connector publishes the linked session.
	var link *LinkedSession
	deadline := time.After(3 * time.Second)
poll:
	for {
		select {
		case <-deadline:
			t.Fatal("connector did not publish a session")
		default:
		}
		if l, ok := conn.Snapshot(); ok {
			link = l
			break poll
		}
		time.Sleep(5 * time.Millisecond)
	}

	if link == nil {
		t.Fatal("no published link")
	}
	if link.Session.Role != config.ModeExit {
		t.Fatalf("session role = %v, want exit", link.Session.Role)
	}

	// Cancel: Run must return the parent's error.
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

// TestConnectorRetriesWhileFailing points the connector at an unreachable relay
// so every authenticate attempt fails; the connector must keep retrying until
// the context is cancelled rather than publishing a session or surfacing early.
func TestConnectorRetriesWhileFailing(t *testing.T) {
	auth := Authenticator{
		Addr: "127.0.0.1:1", Role: config.ModeProxy, Token: "tok",
		Timing: fastTiming(), ServerName: "localhost",
		RootCAs: x509.NewCertPool(), MinTLSVersion: DefaultMinTLSVersion,
	}
	conn := NewConnector(auth, quietLogger(), &net.Dialer{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- conn.Run(ctx) }()

	time.Sleep(250 * time.Millisecond)
	if _, ok := conn.Snapshot(); ok {
		t.Fatal("connector unexpectedly published a session while dial failing")
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

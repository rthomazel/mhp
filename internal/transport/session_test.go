package transport

import (
	"net"
	"testing"
	"time"

	"github.com/rthomazel/mhp/internal/config"
)

// makePair returns a client+server session pair connected over net.Pipe so the
// stream and close behaviors can be exercised end to end.
func makePair(t *testing.T) (*Session, *Session) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	timing := DefaultTiming
	clientMux, err := newClientSession(clientConn, timing)
	if err != nil {
		t.Fatalf("client session: %v", err)
	}
	serverMux, err := newServerSession(serverConn, timing)
	if err != nil {
		t.Fatalf("server session: %v", err)
	}
	client := newSession(clientConn, clientMux, config.ModeProxy, "client")
	server := newSession(serverConn, serverMux, config.ModeExit, "server")
	if client == nil || server == nil {
		t.Fatal("nil session from makePair")
	}
	return client, server
}

func TestNewSessionNilGuard(t *testing.T) {
	if newSession(nil, nil, config.ModeExit, "x") != nil {
		t.Fatal("want nil for nil args")
	}
}

func TestOpenAcceptStream(t *testing.T) {
	client, server := makePair(t)
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	_, err := client.OpenStream()
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	got, err := server.AcceptStream()
	if err != nil {
		t.Fatalf("AcceptStream: %v", err)
	}
	_ = got
}

func TestCloseCascadesToClosingChannel(t *testing.T) {
	client, server := makePair(t)
	defer func() { _ = server.Close() }()

	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-client.Done():
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("closing channel never closed after Close")
	}
}

func TestUptimeAndHealthySince(t *testing.T) {
	client, server := makePair(t)
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	if client.Uptime() < 0 {
		t.Fatal("uptime must be non-negative")
	}
	if !client.HealthySince(0) {
		t.Fatal("session is always healthy past 0")
	}
	if server.HealthySince(time.Hour) {
		t.Fatal("brand-new session is not healthy past 1h")
	}
}

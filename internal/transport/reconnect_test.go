package transport

import (
	"bytes"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rthomazel/mhp/internal/config"
)

// blackholeConn wraps a net.Conn so the peer on the far side can neither read
// nor write through it. Writes are swallowed (nothing ever reaches the peer)
// and reads drain whatever the peer sends (local writes therefore complete).
//
// It models a NAT-expired connection: the local side can still *emit* bytes,
// but they fall into the void and nothing ever comes back. That is precisely
// the half-open state where yamux keepalive is the only way to notice the peer
// is gone — exactly the situation Thom hit on the VPS, where the session died
// during idle and nothing reacted until a new stream tripped over it.
type blackholeConn struct {
	net.Conn
}

func (b *blackholeConn) Read(p []byte) (int, error) {
	// Consume whatever the peer sent so local writes can complete, but never
	// hand any of it back to the peer.
	n, err := b.Conn.Read(p)
	return n, err
}

func (b *blackholeConn) Write(p []byte) (int, error) {
	// Swallow: never deliver anything to the peer.
	return len(p), nil
}

// TestDoneFiresThroughKeepaliveAgainstDeadPeer reproduces the VPS symptom:
// a session whose peer silently vanishes (NAT entry expired) must be detected
// reactively through yamux keepalive, not only when a later stream trips over
// it. We drive a real yamux session whose far end is a black hole — it can
// receive the client's keepalive probes but never answers — and assert the
// client's Done() fires within a bounded window rather than hanging.
func TestDoneFiresThroughKeepaliveAgainstDeadPeer(t *testing.T) {
	clientConn, serverConn := net.Pipe()

	// Fast keepalive so the test exercises the detection window quickly.
	timing := DefaultTiming
	timing.KeepAliveInterval = 150 * time.Millisecond
	timing.PingTimeout = 150 * time.Millisecond

	// Far side is a black hole: it can read the client's probes (so the client
	// can send) but never replies, so the client's keepalive must time out.
	serverMux, err := newServerSession(&blackholeConn{serverConn}, timing, quietLogger())
	if err != nil {
		t.Fatalf("server session: %v", err)
	}
	defer func() { _ = serverMux.Close() }()

	clientMux, err := newClientSession(clientConn, timing, quietLogger())
	if err != nil {
		t.Fatalf("client session: %v", err)
	}
	defer func() { _ = clientMux.Close() }()

	client := newSession(clientConn, clientMux, config.ModeExit, "client")

	window := timing.KeepAliveInterval + timing.PingTimeout + time.Second
	select {
	case <-client.Done():
		// ok: the client noticed its peer vanished through keepalive alone.
	case <-time.After(window):
		t.Fatalf("Done() never fired within %v after peer became unreachable", window)
	}
}

// lockedBuffer is a concurrency-safe bytes.Buffer for asserting what a logger
// captured. slog handlers may be written from multiple goroutines, so all
// access is serialized.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// captureLogger returns a *slog.Logger that records every emitted event into a
// synchronized buffer, plus a handle to read what was captured.
func captureLogger() (*slog.Logger, *lockedBuffer) {
	lb := &lockedBuffer{}
	return slog.New(slog.NewTextHandler(lb, &slog.HandlerOptions{Level: slog.LevelDebug})), lb
}

// TestKeepaliveFailureEmittedWhenLoggerWired verifies that a yamux keepalive
// timeout surfaces through the project logger rather than being silently
// discarded. Operators rely on this to confirm keepalive is actually probing;
// without it the only symptom of a silent keepalive is a 2-minute hang, exactly
// what Thom saw on the VPS. The peer is a black hole, so the client's probes
// are consumed but never answered and its keepalive must time out.
func TestKeepaliveFailureEmittedWhenLoggerWired(t *testing.T) {
	clientConn, serverConn := net.Pipe()

	timing := DefaultTiming
	timing.KeepAliveInterval = 150 * time.Millisecond
	timing.PingTimeout = 150 * time.Millisecond

	// Black-hole the server side so the client's keepalive probes are consumed
	// but never answered.
	serverMux, err := newServerSession(&blackholeConn{serverConn}, timing, quietLogger())
	if err != nil {
		t.Fatalf("server session: %v", err)
	}
	defer func() { _ = serverMux.Close() }()

	// Wire a capturing logger to the client so yamux protocol logs flow through.
	log, lb := captureLogger()
	clientMux, err := newClientSession(clientConn, timing, log)
	if err != nil {
		t.Fatalf("client session: %v", err)
	}
	defer func() { _ = clientMux.Close() }()

	// The keepalive probe fires after KeepAliveInterval and fails after an
	// additional PingTimeout; give ample headroom.
	deadline := time.After(3 * time.Second)
	for {
		if strings.Contains(lb.String(), "keepalive failed") {
			return // success: the diagnostic reached the logger
		}
		select {
		case <-deadline:
			t.Fatalf("keepalive failure not logged within 3s; buffer:\n%s", lb.String())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

package relay

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/hashicorp/yamux"

	"github.com/rthomazel/mhp/internal/config"
)

// muxPair returns (client, server). For the proxy leg the browser is the client
// and opens streams that the relay (server) accepts. For the exit leg the relay
// is the client and opens streams that the exit (server) accepts, so the
// registry stores relayExit and the bridge drives relayExit.OpenStream().
func bridgeSetup(t *testing.T) (*Bridge, *yamux.Session, *yamux.Session, *yamux.Session, *yamux.Session) {
	t.Helper()

	reg := NewRegistry()

	// proxy leg: browser opens, relay accepts.
	browser, relayProxy := muxPair(t)
	// exit leg: relay opens, exit accepts.
	relayExit, exit := muxPair(t)

	reg.Register(config.ModeExit, relayExit)

	br := NewBridge(reg, nil)
	return br, browser, relayProxy, relayExit, exit
}

// TestBridgePairsStreams verifies a browser-opened stream is paired with a
// relay-opened exit stream and bytes flow both ways.
func TestBridgePairsStreams(t *testing.T) {
	br, browser, relayProxy, _, exit := bridgeSetup(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = br.ServeProxy(ctx, relayProxy) }()

	browserStream, err := browser.OpenStream()
	if err != nil {
		t.Fatalf("browser open stream: %v", err)
	}

	// The bridge accepted the browser stream and opened an exit stream, so the
	// exit now sees its paired stream.
	exitStream, err := exit.AcceptStream()
	if err != nil {
		t.Fatalf("exit accept paired stream: %v", err)
	}

	const payload = "payload-from-browser"
	if _, err := browserStream.Write([]byte(payload)); err != nil {
		t.Fatalf("write on browser leg: %v", err)
	}

	exitStream.SetReadDeadline(time.Now().Add(2 * time.Second))
	got := make([]byte, len(payload))
	n, err := exitStream.Read(got)
	if err != nil {
		t.Fatalf("read on exit leg: %v", err)
	}
	if !bytes.Equal(got[:n], []byte(payload)) {
		t.Fatalf("copy mismatch: got %q want %q", got[:n], payload)
	}
}

// TestBridgeNoExitClosesStream verifies that with no exit registered the relay
// closes the incoming stream immediately rather than queuing it.
func TestBridgeNoExitClosesStream(t *testing.T) {
	reg := NewRegistry()
	browser, relayProxy := muxPair(t)
	reg.Register(config.ModeProxy, browser)
	br := NewBridge(reg, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = br.ServeProxy(ctx, relayProxy) }()

	browserStream, err := browser.OpenStream()
	if err != nil {
		t.Fatalf("browser open stream: %v", err)
	}
	defer browserStream.Close()

	browserStream.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	_, err = browserStream.Read(buf)
	if err == nil {
		t.Fatalf("expected EOF when relay closes stream with no exit")
	}
}

// TestBridgeRejectsReverseStream verifies a stream the exit opens toward the
// relay is closed, not paired back to the browser.
func TestBridgeRejectsReverseStream(t *testing.T) {
	br, _, _, relayExit, exit := bridgeSetup(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = br.ServeExit(ctx, relayExit) }()

	// The exit opens a stream toward the relay (reverse direction).
	reverse, err := exit.OpenStream()
	if err != nil {
		t.Fatalf("exit open reverse stream: %v", err)
	}
	defer reverse.Close()

	// ServeExit accepts and immediately closes it, so the exit sees EOF.
	reverse.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	_, err = reverse.Read(buf)
	if err == nil {
		t.Fatalf("expected EOF after ServeExit rejects a reverse stream")
	}
	if !isClosed(err) {
		t.Fatalf("expected close after ServeExit rejects a reverse stream, got %v", err)
	}
}

// isClosed reports whether err represents a stream that has been torn down
// (EOF on a half-close, or a reset/closed connection on a forced close).
func isClosed(err error) bool {
	return err == io.EOF ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, io.ErrClosedPipe)
}

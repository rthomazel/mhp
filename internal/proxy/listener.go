// Package proxy implements the browser-facing leg of MHP: a loopback SOCKS5
// listener whose connections become a single relay stream each. The proxy
// speaks no SOCKS5 of its own — it forwards the browser's SOCKS5 exchange and
// the subsequent payload bytes verbatim to the exit, which owns the SOCKS5
// server. The relay bridge carries those bytes unchanged, so Internet egress
// always originates at the exit, never at the proxy.
//
// Each accepted local TCP connection snapshots the current relay session,
// opens one stream toward the relay, and duplex-copies between the local
// connection and that stream. The loopback listener stays up regardless of
// relay availability: while the client is mid-reconnect the proxy simply
// fails new connections promptly rather than queuing them.
package proxy

import (
	"context"
	"errors"
	"log/slog"
	"net"

	"github.com/rthomazel/mhp/internal/stream"
	"github.com/rthomazel/mhp/internal/transport"
)

// MaxPending is the capacity of the admission semaphore. It caps the number of
// simultaneously bridged browser connections at the proxy, as the plan pins.
const MaxPending = 64

// Listener accepts browser TCP connections on a loopback address and bridges
// each into the current relay session. It is intentionally decoupled from the
// connector's reconnect loop: it only ever reaches the freshest session through
// Snapshot, so it degrades gracefully across outages.
type Listener struct {
	ln        net.Listener
	connector *transport.Connector
	logger    *slog.Logger
	sem       chan struct{}
}

// New returns a Listener that accepts on ln and bridges into the connector's
// current session. The admission capacity is fixed at MaxPending; passing a
// value here is unnecessary because the semaphore is sized at construction.
func New(ln net.Listener, connector *transport.Connector, logger *slog.Logger) *Listener {
	if logger == nil {
		logger = slog.Default()
	}
	return &Listener{
		ln:        ln,
		connector: connector,
		logger:    logger,
		sem:       make(chan struct{}, MaxPending),
	}
}

// ListenAddr returns the address the listener is bound to. Useful for tests
// that must learn the ephemeral port a :0 listener was granted.
func (l *Listener) ListenAddr() net.Addr { return l.ln.Addr() }

// Run accepts browser connections until ctx is cancelled or the listener is
// closed. It returns when the listener stops; accept failures never abort the
// loop. New connections admitted while no session is available are closed
// promptly rather than queued, and the global bridge count never exceeds the
// admission semaphore capacity.
//
// A background goroutine watches ctx and closes the listener on cancellation so
// the parked Accept returns and Run can unwind. Without this a cancelled
// context would leave Run stuck in Accept forever.
func (l *Listener) Run(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = l.ln.Close()
	}()

	for {
		conn, err := l.ln.Accept()
		if err != nil {
			// A closed listener or cancelled context is the only reason to stop.
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				return err
			}
		}
		go l.serve(ctx, conn)
	}
}

// serve bridges a single browser connection. It admits work through the
// semaphore, snapshots the current session, opens one relay stream, and
// duplex-copies until either side finishes or the session ends.
func (l *Listener) serve(ctx context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()

	// Non-blocking admission: if the proxy is already bridging at capacity,
	// close this connection immediately rather than letting the accept loop
	// stall behind it.
	if !l.acquire() {
		l.logger.Debug("proxy: connection limit reached, refusing",
			"peer", conn.RemoteAddr().String())
		return
	}
	defer l.release()

	link, ok := l.connector.Snapshot()
	if !ok {
		// Mid-reconnect or not yet connected: fail promptly, never queue.
		l.logger.Debug("proxy: no relay session, closing browser connection")
		return
	}

	relayConn, err := link.Session.OpenStream()
	if err != nil {
		l.logger.Warn("proxy: failed to open relay stream", "error", err)
		return
	}

	// Duplex ties the bridge's lifetime to the session: when the session ends
	// or ctx is cancelled the copy unwinds, so a relay outage tears down the
	// browser flow without leaking the goroutine.
	dup := stream.NewDuplex(conn, relayConn)
	if err := dup.Run(link.Ctx); err != nil && !errors.Is(err, context.Canceled) {
		l.logger.Warn("proxy: bridge closed", "error", err)
	}
}

// acquire attempts a non-blocking entry into the admission gate.
func (l *Listener) acquire() bool {
	select {
	case l.sem <- struct{}{}:
		return true
	default:
		return false
	}
}

// release returns a slot to the admission gate.
func (l *Listener) release() {
	select {
	case <-l.sem:
	default:
	}
}

package exit

import (
	"context"
	"log/slog"
	"net"
	"time"

	"github.com/rthomazel/mhp/internal/transport"
)

// Broker is the exit-side driver. It watches the transport connector's published
// linked session, and for each relay-opened stream passes it to the SOCKS5
// handler. It rides the connector's reconnect loop: when a session ends it
// grabs the next published session, so the exit keeps serving as long as the
// connector can re-establish a link. It never talks to the relay directly; the
// connector owns the dial, handshake, and reconnection.
type Broker struct {
	connector *transport.Connector
	handler   *Handler
	logger    *slog.Logger
}

// NewBroker returns a Broker that serves relay-opened streams through handler,
// reaching the current session via connector. A nil logger falls back to the
// default logger.
func NewBroker(connector *transport.Connector, handler *Handler, logger *slog.Logger) *Broker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Broker{connector: connector, handler: handler, logger: logger}
}

// sessionStall bounds how often the broker polls while no session is published
// or the current one has ended.
const sessionStall = 100 * time.Millisecond

// Run accepts relay-opened streams for the current linked session until ctx is
// cancelled. It returns only when ctx is done; session churn is handled by
// re-snapshotting the connector's newest link.
func (b *Broker) Run(ctx context.Context) error {
	var last *transport.LinkedSession
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		link, ok := b.connector.Snapshot()
		if !ok || link == last {
			// No session, or the session is unchanged since the last attempt
			// (typically one that just ended and is awaiting the next link).
			// Wait briefly to avoid a hot spin.
			if !sleepUntil(ctx, sessionStall) {
				return ctx.Err()
			}
			continue
		}
		last = link

		if err := b.serveSession(ctx, link); err != nil {
			// The session ended or ctx was cancelled; loop to the next link.
			b.logger.Debug("exit: session serve loop ended", "error", err)
		}
	}
}

// serveSession accepts relay-opened streams on link until the session ends
// (natural peer death or shutdown) and serves SOCKS5 on each. It returns when
// the session is gone so the caller can grab the next published link.
func (b *Broker) serveSession(ctx context.Context, link *transport.LinkedSession) error {
	// AcceptStream blocks until a stream arrives or the session ends. We watch
	// both ctx and the session's CloseChan so a natural peer drop returns here
	// rather than parking on AcceptStream forever.
	acceptCh := make(chan net.Conn, 1)
	go func() {
		conn, err := link.Session.AcceptStream()
		if err == nil {
			select {
			case acceptCh <- conn:
			default:
				_ = conn.Close()
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			// Shutdown: close the session to unblock the accept goroutine.
			_ = link.Session.Close()
			return ctx.Err()
		case <-link.Session.Done():
			// Session ended naturally; closing is idempotent and unblocks the
			// accept goroutine, which then exits.
			_ = link.Session.Close()
			return nil
		case conn := <-acceptCh:
			b.serveConn(link, conn)
		}
	}
}

// serveConn runs one SOCKS5 request to completion on its own goroutine so the
// accept loop stays free for the next relay-opened stream. Errors are logged at
// debug level: a failed request is expected (denied destination, dial error),
// not a fatal condition.
func (b *Broker) serveConn(link *transport.LinkedSession, conn net.Conn) {
	go func() {
		if err := b.handler.ServeConn(link.Ctx, conn); err != nil {
			b.logger.Debug("exit: SOCKS5 request ended", "error", err)
		}
	}()
}

// sleepUntil blocks for d or until ctx is cancelled, returning false if the
// context fired so callers can stop promptly. It is the broker's only throttle,
// turning "no session available yet" into a polite poll rather than a hot spin.
func sleepUntil(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

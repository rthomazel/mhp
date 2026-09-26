package transport

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync/atomic"
	"time"
)

// LinkedSession couples an authenticated Session with the context that dies
// when the session ends. Servers that ride a session use Ctx so their work is
// cancelled the instant the session tears down, and Snapshot to reach the
// current link.
type LinkedSession struct {
	// Session is the authenticated, muxed connection to the relay.
	Session *Session
	// Ctx is derived from the connector's parent and is cancelled when the
	// session ends or the parent is cancelled.
	Ctx context.Context
	// cancel closes Ctx; unexported so callers cannot keep a dead session
	// alive past its useful life.
	cancel context.CancelFunc
	// done is closed once the session has ended; unexported for the same
	// reason as cancel.
	done chan struct{}
}

// Connector drives the client-side reconnect journey for a single role. It
// authenticates, publishes the current session through Snapshot, and retries
// forever with capped, jittered backoff. A new same-role registration
// replaces the old one; the connector only ever hands out the newest link.
type Connector struct {
	// auth dials, authenticates, and opens a mux. Built once from config.
	auth Authenticator
	// logger records establishment and reconnection events.
	logger *slog.Logger
	// dialer is the TCP dialer handed to every attempt.
	dialer *net.Dialer
	// current holds the newest linked session, swapped atomically so a
	// superseded link is never served.
	current atomic.Pointer[LinkedSession]
}

// NewConnector returns a connector that authenticates with auth, logs through
// logger, and dials through dialer (a fresh &net.Dialer is used when nil).
func NewConnector(auth Authenticator, logger *slog.Logger, dialer *net.Dialer) *Connector {
	if logger == nil {
		logger = slog.Default()
	}
	if dialer == nil {
		dialer = &net.Dialer{}
	}
	return &Connector{auth: auth, logger: logger, dialer: dialer}
}

// Snapshot returns the current linked session, or ok=false when none is
// registered. A false result means the client is mid-backoff or reconnecting,
// so servers must fail new work promptly rather than queue it.
func (c *Connector) Snapshot() (*LinkedSession, bool) {
	link := c.current.Load()
	if link == nil {
		return nil, false
	}
	return link, true
}

// clearIf drops the link only if it is still the current one, so a stale
// session ending cannot evict the registration that replaced it.
func (c *Connector) clearIf(cur *LinkedSession) {
	c.current.CompareAndSwap(cur, nil)
}

// Run authenticates, publishes the session, and serves until the parent
// context is cancelled. It retries forever, resetting the backoff floor only
// after a session has stayed healthy for the configured window. The caller's
// context governs the whole journey; session teardown cancels the link's
// context so its workers unwind.
func (c *Connector) Run(parent context.Context) error {
	backoff := c.auth.Timing.MinBackoff
	for {
		link, err := c.connect(parent)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return parent.Err()
			}
			c.logger.Warn("reconnect: authentication or transport failed",
				"reason", err.Error())
			if err := c.waitBackoff(parent, backoff); err != nil {
				return err
			}
			backoff = min(backoff*2, c.auth.Timing.MaxBackoff)
			continue
		}

		c.current.Store(link)
		c.logger.Info("session established",
			"role", string(c.auth.Role), "session_id", link.Session.ID)

		if err := c.serve(parent, link); err != nil {
			return err
		}
		c.clearIf(link)

		// Reset the backoff floor only if the session proved healthy for
		// long enough; a quick flap keeps exponential growth.
		if link.Session.HealthySince(c.auth.Timing.HealthyWindow) {
			backoff = c.auth.Timing.MinBackoff
		}
	}
}

// connect performs a single authenticate attempt and wraps the result in a
// linked session whose context is cancelled when the session ends.
func (c *Connector) connect(parent context.Context) (*LinkedSession, error) {
	sess, err := c.auth.authenticate(parent, c.logger, c.dialer)
	if err != nil {
		return nil, err
	}

	link := &LinkedSession{Session: sess, done: make(chan struct{})}
	ctx, cancel := context.WithCancel(parent)
	link.Ctx = ctx
	link.cancel = cancel

	go func() {
		defer close(link.done)
		select {
		case <-sess.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	return link, nil
}

// serve blocks until the session ends or the parent is cancelled, then
// unblocks any waiter riding this link.
func (c *Connector) serve(parent context.Context, link *LinkedSession) error {
	select {
	case <-link.done:
		return nil
	case <-parent.Done():
		link.cancel()
		return parent.Err()
	}
}

// waitBackoff sleeps for d, returning early with the parent error if it is
// cancelled meanwhile.
func (c *Connector) waitBackoff(parent context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-parent.Done():
		return parent.Err()
	case <-timer.C:
		return nil
	}
}

package exit

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	socks5 "github.com/things-go/go-socks5"
	statute "github.com/things-go/go-socks5/statute"
)

// Dialer is the minimal dialing surface the exit needs to open a destination.
// *net.Dialer satisfies it; tests supply a fake so handleConnect can be
// exercised without a live network.
type Dialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// Options configures a Handler. Zero fields fall back to sensible, plan-derived
// defaults via defaults().
type Options struct {
	// SetupTimeout bounds the whole SOCKS setup: method selection, resolution,
	// access validation, and the destination dial. After CONNECT succeeds this
	// budget is discarded so browsing data is never time-boxed by it.
	SetupTimeout time.Duration
	// DialTimeout bounds the destination dial alone, within SetupTimeout.
	DialTimeout time.Duration
	// ResolveTimeout bounds a single DNS lookup.
	ResolveTimeout time.Duration
	// ResolveBound caps concurrent lookups.
	ResolveBound int

	Logger *slog.Logger
	// Dialer dials every destination. Nil uses a fresh &net.Dialer.
	Dialer Dialer
}

func (o *Options) defaults() {
	if o.SetupTimeout <= 0 {
		o.SetupTimeout = 15 * time.Second
	}
	if o.DialTimeout <= 0 {
		o.DialTimeout = 10 * time.Second
	}
	if o.ResolveTimeout <= 0 {
		o.ResolveTimeout = 5 * time.Second
	}
	if o.ResolveBound < 1 {
		o.ResolveBound = 16
	}
}

// Handler runs the SOCKS5 server on behalf of each relay stream. It is the only
// place MHP speaks SOCKS5: the browser's SOCKS5 request is parsed here, the
// destination is validated against the Internet-only policy, and the exit dials
// it from its own network namespace so egress originates at the exit, never at
// the relay or the proxy.
type Handler struct {
	opts Options
	pol  *policy
}

// New returns a Handler ready to serve SOCKS5 over relay streams.
func New(opts Options) *Handler {
	opts.defaults()
	return &Handler{opts: opts, pol: newPolicy()}
}

// socksLogger adapts an *slog.Logger to the SOCKS5 library's Logger interface so
// the library's internal diagnostics land in MHP's pipeline.
type socksLogger struct{ l *slog.Logger }

func (s socksLogger) Errorf(format string, a ...any) {
	if s.l == nil {
		return
	}
	s.l.Error(fmt.Sprintf(format, a...))
}

// ServeConn configures a throwaway SOCKS5 server bound to this stream's
// session and setup contexts, then serves the browser's request on conn. It is
// called once per accepted relay stream.
func (h *Handler) ServeConn(sessionCtx context.Context, conn net.Conn) error {
	setupCtx, cancel := context.WithTimeout(sessionCtx, h.opts.SetupTimeout)
	defer cancel()

	resolver := newBoundedResolver(setupCtx, h.opts.ResolveBound, h.opts.ResolveTimeout, h.opts.Logger)

	// The library invokes the connect handle with context.Background, so we
	// capture sessionCtx and setupCtx in the closure to own all timing. The
	// writer the library hands is a net.Conn at runtime; assert it so the
	// deadline setter stays available after the setup boundary.
	connectHandle := func(_ context.Context, writer io.Writer, req *socks5.Request) error {
		nc, ok := writer.(net.Conn)
		if !ok {
			return fmt.Errorf("exit: unexpected connection wrapper")
		}
		return h.handleConnect(sessionCtx, setupCtx, nc, req)
	}

	srv := socks5.NewServer(
		socks5.WithResolver(resolver),
		socks5.WithRule(connectOnlyRule{}),
		socks5.WithConnectHandle(connectHandle),
		socks5.WithLogger(socksLogger{l: h.opts.Logger}),
	)
	return srv.ServeConn(conn)
}

// handleConnect is the SOCKS5 CONNECT handler. It validates the (already
// resolved) destination, dials it from the exit, replies success with the
// actual egress address, then bridges the two directions. The library passes a
// context.Background here, so all timing derives from the captured session and
// setup contexts instead.
func (h *Handler) handleConnect(sessionCtx, setupCtx context.Context, writer net.Conn, req *socks5.Request) error {
	dest := req.RawDestAddr
	ip := dest.IP
	if v4 := ip.To4(); v4 != nil {
		ip = v4 // normalise IPv4-mapped IPv6 before validation
	}

	if !h.pol.allowed(ip) {
		// Access denied before we ever touch the network.
		_ = socks5.SendReply(writer, statute.RepRuleFailure, nil)
		return fmt.Errorf("exit: destination %s refused by policy", ip)
	}

	// Dial from the exit within the setup budget; the dialer honors the session
	// context so a torn-down session unblocks the dial immediately.
	dialer := h.opts.Dialer
	if dialer == nil {
		dialer = &net.Dialer{}
	}
	target, err := dialer.DialContext(setupCtx, "tcp", net.JoinHostPort(ip.String(), strconv.Itoa(dest.Port)))
	if err != nil {
		_ = socks5.SendReply(writer, statute.RepHostUnreachable, nil)
		return fmt.Errorf("exit: dial %s: %w", ip, err)
	}

	// Success reports the destination socket's bound address — the exit's real
	// egress address — not the relay address the stream arrived on.
	if err := socks5.SendReply(writer, statute.RepSuccess, target.LocalAddr()); err != nil {
		_ = target.Close()
		return fmt.Errorf("exit: send reply: %w", err)
	}

	// CONNECT succeeded: the setup budget has done its job. Drop it. Subsequent
	// browsing data rides sessionCtx only, so it is never truncated by a short
	// setup deadline.
	forward(sessionCtx, writer, req.Reader, target, h.opts.Logger)
	return nil
}

// forward pipes the SOCKS payload between the browser stream (reader/writer) and
// the dialed destination. It mirrors the library's half-close discipline: when
// the client stops sending it FINs the destination's write side while keeping
// the response readable, and vice versa. sessionCtx governs the transfer —
// cancellation stamps a past deadline on both ends to unblock the still-running
// copy, and every goroutine is joined before returning.
func forward(sessionCtx context.Context, writer net.Conn, reader io.Reader, target net.Conn, logger *slog.Logger) {
	var cp sync.WaitGroup
	cp.Add(1)
	go func() {
		defer cp.Done()
		// client -> target. io.Copy drains the SOCKS library's buffered reader,
		// so read-ahead bytes are never lost.
		if _, err := io.Copy(target, reader); err != nil {
			logger.Debug("exit: client->target copy ended", "error", err)
		}
		// Client half-closed: FIN the destination write side, keep reading.
		if cw, ok := target.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
	}()

	// Unblock both copies if the session is torn down mid-transfer.
	stop := make(chan struct{})
	go func() {
		select {
		case <-sessionCtx.Done():
			dead := time.Now().Add(-time.Second)
			_ = target.SetDeadline(dead)
			_ = writer.SetDeadline(dead)
		case <-stop:
		}
	}()

	// target -> client.
	if _, err := io.Copy(writer, target); err != nil {
		logger.Debug("exit: target->client copy ended", "error", err)
	}
	close(stop)

	// Response drained. Unblock any still-pending client->target copy by
	// stamping a past deadline on the browser stream, then join.
	_ = writer.SetDeadline(time.Now().Add(-time.Second))
	cp.Wait()
}

// connectOnlyRule permits the CONNECT command and rejects BIND/ASSOCIATE, as
// the plan mandates a CONNECT-only exit.
type connectOnlyRule struct{}

func (connectOnlyRule) Allow(ctx context.Context, req *socks5.Request) (context.Context, bool) {
	if req.Command == statute.CommandConnect {
		return ctx, true
	}
	return ctx, false
}

// the remainder of this file supplies the two context handles the connect
// handle captures. They are kept out of the closure literal for readability and
// so tests can substitute derived contexts.

// Package proxy implements the browser-facing leg of MHP: a SOCKS5 listener
// whose authenticated requests become relay streams. Authentication happens
// locally; the exit receives the request after the SOCKS5 negotiation.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"

	"github.com/rthomazel/mhp/internal/stream"
	"github.com/rthomazel/mhp/internal/transport"
	socks5 "github.com/things-go/go-socks5"
)

// MaxPending is the capacity of the admission semaphore.
const MaxPending = 256

// Listener accepts browser TCP connections and serves local SOCKS5
// authentication before bridging each request into the current relay session.
type Listener struct {
	ln        net.Listener
	connector *transport.Connector
	logger    *slog.Logger
	sem       chan struct{}
	username  string
	password  string
}

// New returns a Listener. Empty credentials disable authentication.
func New(ln net.Listener, connector *transport.Connector, logger *slog.Logger, credentials ...string) *Listener {
	if logger == nil {
		logger = slog.Default()
	}
	var username, password string
	if len(credentials) >= 2 {
		username, password = credentials[0], credentials[1]
	}
	return &Listener{
		ln:        ln,
		connector: connector,
		logger:    logger,
		sem:       make(chan struct{}, MaxPending),
		username:  username,
		password:  password,
	}
}

// ListenAddr returns the listener address.
func (l *Listener) ListenAddr() net.Addr { return l.ln.Addr() }

// Run accepts browser connections until ctx is cancelled or the listener stops.
func (l *Listener) Run(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = l.ln.Close()
	}()
	for {
		conn, err := l.ln.Accept()
		if err != nil {
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

func (l *Listener) serve(ctx context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()
	if !l.acquire() {
		l.logger.Debug("proxy: connection limit reached, refusing", "peer", conn.RemoteAddr())
		return
	}
	defer l.release()

	server := socks5.NewServer(
		socks5.WithAuthMethods(l.authMethods()),
		socks5.WithConnectHandle(func(_ context.Context, writer io.Writer, request *socks5.Request) error {
			return l.bridge(ctx, conn, writer, request)
		}),
	)
	if err := server.ServeConn(conn); err != nil && !errors.Is(err, context.Canceled) {
		l.logger.Debug("proxy: SOCKS5 connection ended", "error", err)
	}
}

func (l *Listener) bridge(ctx context.Context, conn net.Conn, writer io.Writer, request *socks5.Request) error {
	link, ok := l.connector.Snapshot()
	if !ok {
		return fmt.Errorf("proxy: no relay session")
	}
	relayConn, err := link.Session.OpenStream()
	if err != nil {
		return fmt.Errorf("proxy: open relay stream: %w", err)
	}
	defer func() { _ = relayConn.Close() }()

	if err := forwardRequest(relayConn, request); err != nil {
		return err
	}
	logger := l.logger.With("session_id", link.Session.ID)
	started := time.Now()
	defer func() { logger.Debug("proxy: stream ended", "elapsed_ms", time.Since(started).Milliseconds()) }()
	return stream.NewDuplex(&requestConn{Conn: conn, reader: request.Reader}, relayConn).Run(link.Ctx)
}

func forwardRequest(dst io.Writer, request *socks5.Request) error {
	if _, err := dst.Write(request.Bytes()); err != nil {
		return fmt.Errorf("proxy: forward request: %w", err)
	}
	return nil
}

type requestConn struct {
	net.Conn
	reader io.Reader
}

func (c *requestConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

// CloseWrite fully closes the browser connection when the relay stream ends.
// Keeping the local TCP connection half-open here makes Duplex wait for its
// drain timeout if the SOCKS client is still waiting for a response.
func (c *requestConn) CloseWrite() error { return c.Conn.Close() }

func (l *Listener) authMethods() []socks5.Authenticator {
	if l.username == "" && l.password == "" {
		return []socks5.Authenticator{&socks5.NoAuthAuthenticator{}}
	}
	return []socks5.Authenticator{&socks5.UserPassAuthenticator{
		Credentials: socks5.StaticCredentials{l.username: l.password},
	}}
}

func (l *Listener) acquire() bool {
	select {
	case l.sem <- struct{}{}:
		return true
	default:
		return false
	}
}

func (l *Listener) release() {
	select {
	case <-l.sem:
	default:
	}
}

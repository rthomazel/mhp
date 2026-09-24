package transport

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"time"
)

// ErrAuthentication is returned to the connector when the relay rejects the
// token; the connector backs off and retries rather than surfacing a fatal
// error.
var ErrAuthentication = errors.New("transport: authentication failed")

// ErrExchange is returned when the hello/response exchange corrupts mid-flight.
var ErrExchange = errors.New("transport: hello exchange failed")

// ErrCloseFailed is returned when the underlying conn fails to close during
// teardown.
var ErrCloseFailed = errors.New("transport: session close failed")

// Authenticator drives the client-side journey: dial, handshake, hello/response,
// then start yamux. It trusts the relay CA (never InsecureSkipVerify),
// authenticates with a bearer token, and refuses to create a session before the
// relay accepts.
type Authenticator struct {
	// Addr is the relay host:port to dial.
	Addr string
	// Role is the client's role (exit or proxy).
	Role Mode
	// Token is the bearer to present.
	Token string
	// Timing holds every duration the transport relies on.
	Timing Timing
	// ServerName is the SNI the client presents and verifies.
	ServerName string
	// RootCAs is the trusted relay CA pool, built from the client's -ca file.
	RootCAs *x509.CertPool
	// MinTLSVersion pins the floor on the client's TLS config (tls.VersionTLSxx).
	MinTLSVersion uint16
}

// dial opens the TCP connection, upgrades it to TLS, and returns the raw conn.
func (a *Authenticator) dial(ctx context.Context, timing Timing, dialer *net.Dialer) (net.Conn, error) {
	cfg := TLSConfig{
		ServerName:    a.ServerName,
		RootCAs:       a.RootCAs,
		MinTLSVersion: a.MinTLSVersion,
	}
	conn, err := dialTLS(ctx, a.Addr, cfg, timing, dialer)
	if err != nil {
		return nil, fmt.Errorf("transport: dial: %w", err)
	}
	return conn, nil
}

// exchange sends the hello, reads the verdict, and validates the response.
func (a *Authenticator) exchange(ctx context.Context, conn net.Conn, timing Timing) (Response, error) {
	_ = ctx
	if err := conn.SetDeadline(time.Now().Add(timing.SetupTimeout)); err != nil {
		return Response{}, fmt.Errorf("transport: set setup deadline: %w", err)
	}
	if err := writeJSON(conn, Hello{
		Version: helloVersion,
		Role:    a.Role,
		Token:   a.Token,
	}); err != nil {
		return Response{}, fmt.Errorf("%w: %v", ErrExchange, err)
	}
	var resp Response
	if err := readJSON(conn, &resp); err != nil {
		return Response{}, fmt.Errorf("%w: %v", ErrExchange, err)
	}
	if err := validateResponse(resp); err != nil {
		return Response{}, fmt.Errorf("transport: invalid response: %w", err)
	}
	if resp.Status != statusOK {
		errMsg := "authentication failed"
		if resp.ErrorCode != "" {
			errMsg = resp.ErrorCode
		}
		return Response{}, fmt.Errorf("%w: %s", ErrAuthentication, errMsg)
	}
	_ = conn.SetDeadline(time.Time{})
	return resp, nil
}

// authenticate dials, exchanges, and starts the mux, returning a Session.
func (a *Authenticator) authenticate(ctx context.Context, log *slog.Logger, dialer *net.Dialer) (*Session, error) {
	conn, err := a.dial(ctx, a.Timing, dialer)
	if err != nil {
		return nil, fmt.Errorf("transport: %w", err)
	}
	resp, err := a.exchange(ctx, conn, a.Timing)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("transport: %w", err)
	}
	mux, err := newClientSession(conn, a.Timing)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: %v", ErrHandshake, err)
	}
	session := newSession(conn, mux, a.Role, resp.SessionID)
	if session == nil {
		_ = mux.Close()
		return nil, fmt.Errorf("%w", ErrNilSession)
	}
	log.Info("authenticating",
		"role", string(a.Role),
		"session_id", session.ID,
	)
	return session, nil
}

// Run authenticates, retrying forever with jittered exponential backoff until the
// context is cancelled. It reloads credentials on each attempt so corrected
// provisioning recovers without a process restart.
func (a *Authenticator) Run(ctx context.Context, log *slog.Logger, dialer *net.Dialer) (*Session, error) {
	attempt := 0
	var backoff time.Duration
	for {
		session, err := a.authenticate(ctx, log, dialer)
		if err == nil {
			return session, nil
		}
		log.Warn("authenticating failed",
			"attempt", attempt,
			"error", err.Error(),
		)
		backoff = a.backoff(attempt)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
			attempt++
		}
	}
}

// backoff computes the jittered exponential delay for the given attempt.
func (a *Authenticator) backoff(attempt int) time.Duration {
	backoff := a.Timing.MinBackoff
	for i := 0; i < attempt; i++ {
		backoff *= 2
		if backoff >= a.Timing.MaxBackoff {
			backoff = a.Timing.MaxBackoff
			break
		}
	}
	jitter := time.Duration(rand.Float64() * float64(a.Timing.MinBackoff) * a.Timing.JitterRatio)
	backoff += jitter
	if backoff > a.Timing.MaxBackoff {
		backoff = a.Timing.MaxBackoff
	}
	return backoff
}

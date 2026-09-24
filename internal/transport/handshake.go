package transport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
)

// ErrListenTLS is returned when the TLS handshake on the accepted socket fails.
var ErrListenTLS = errors.New("transport: TLS listen failed")

// ErrRecvFailure is returned when the relay cannot read the hello frame.
var ErrRecvFailure = errors.New("transport: receive failure")

// ErrSendFailure is returned when the relay cannot write the verdict frame.
var ErrSendFailure = errors.New("transport: send failure")

// ErrEmptyBearer is returned when a presented token is blank.
var ErrEmptyBearer = errors.New("transport: empty bearer")

// ErrBadRole is returned when the presented role is not one the relay trusts.
var ErrBadRole = errors.New("transport: unexpected role")

// ErrWrongToken is returned when the presented token does not match the
// relay's expected bearer for the role.
var ErrWrongToken = errors.New("transport: wrong token")

// ErrNilSession is returned when the constructed session is nil.
var ErrNilSession = errors.New("transport: nil session")

// Verifier decides which role→bearer pairs the relay trusts. Its map is built
// once at startup from config and read-only afterwards.
type Verifier struct {
	// ExpectedExits maps role to bearer.
	ExpectedExits map[Mode]string
}

// verify checks a presented role/token against the trusted map, rejecting an
// unknown role, an empty bearer, and a mismatched token.
func (v Verifier) verify(role Mode, token string) error {
	expected, ok := v.ExpectedExits[role]
	if !ok {
		return fmt.Errorf("%w: %q", ErrBadRole, role)
	}
	if token == "" {
		return ErrEmptyBearer
	}
	if token != expected {
		return fmt.Errorf("%w: %q", ErrWrongToken, role)
	}
	return nil
}

// Handshake drives the relay-side journey: accept a TLS conn, verify the
// presented bearer, respond with a verdict, and start yamux only on success.
type Handshake struct {
	// TLSConfig carries the relay leaf+key.
	TLSConfig TLSConfig
	// Timing holds the durations the transport relies on.
	Timing Timing
	// Logger is used for diagnostics.
	Logger *slog.Logger
	// Verifier decides which role→bearer pairs the relay trusts.
	Verifier Verifier
}

// handshake accepts one TLS conn, verifies the hello, responds, and starts mux.
func (h *Handshake) handshake(ctx context.Context, raw net.Conn) (*Session, error) {
	tlsConn, err := listenTLS(ctx, raw, h.TLSConfig, h.Timing)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrListenTLS, err)
	}

	var hello Hello
	if err := readJSON(tlsConn, &hello); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRecvFailure, err)
	}
	if err := validateHello(hello); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRecvFailure, err)
	}
	if err := h.Verifier.verify(hello.Role, hello.Token); err != nil {
		// Reply with a statusError so the client fails fast with ErrAuthentication
		// instead of blocking until SetupTimeout. The write is best-effort; the
		// primary error is what the caller observes.
		_ = writeVerdict(tlsConn, "authentication_failed")
		return nil, fmt.Errorf("transport: verify: %w", err)
	}

	sessionID := newSessionID()
	resp := Response{
		Version:   helloVersion,
		Status:    statusOK,
		SessionID: sessionID,
	}
	if err := writeJSON(tlsConn, resp); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSendFailure, err)
	}

	mux, err := newServerSession(tlsConn, h.Timing)
	if err != nil {
		_ = tlsConn.Close()
		return nil, fmt.Errorf("transport: %w", err)
	}

	session := newSession(tlsConn, mux, hello.Role, sessionID)
	if session == nil {
		_ = mux.Close()
		return nil, fmt.Errorf("%w", ErrNilSession)
	}

	if h.Logger != nil {
		h.Logger.Info("handshake complete",
			"role", string(hello.Role),
			"session_id", session.ID,
		)
	}
	return session, nil
}

// writeVerdict sends the relay's verdict frame to the client. On failure it
// carries a bounded, non-sensitive error code so the client can distinguish a
// rejected handshake from a transport error without leaking secrets.
func writeVerdict(conn net.Conn, code string) error {
	return writeJSON(conn, Response{
		Version:   helloVersion,
		Status:    statusError,
		ErrorCode: code,
	})
}

// Run accepts one connection from the listener and returns its session, or nil
// when the context is cancelled before a connection arrives. The relay package
// owns the listener and may call this per accepted conn.
func (h *Handshake) Run(ctx context.Context, ln net.Listener) (*Session, error) {
	for {
		raw, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil, nil
			default:
				return nil, fmt.Errorf("transport: accept: %w", err)
			}
		}
		session, herr := h.handshake(ctx, raw)
		if herr != nil {
			_ = raw.Close()
			if h.Logger != nil {
				h.Logger.Warn("handshake failed", "error", herr.Error())
			}
			continue
		}
		return session, nil
	}
}

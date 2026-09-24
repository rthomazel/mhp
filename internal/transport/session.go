package transport

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
)

// Session is a fully authenticated, muxed connection to the relay. It carries
// exactly one role's worth of streams.
type Session struct {
	// ID is the relay-issued handle.
	ID string
	// Conn is the authenticated TLS connection backing the session.
	Conn net.Conn
	// Mux holds every stream.
	Mux *yamux.Session
	// Role is the client's role (exit or proxy).
	Role Mode
	// Closing is closed once when the session ends.
	closing   chan struct{}
	closeOnce sync.Once
	// StartedAt is the monotonic time the handshake and auth completed.
	startedAt time.Time
}

// newSession builds a Session from an authenticated conn and mux, guarding
// against a nil conn or mux.
func newSession(conn net.Conn, mux *yamux.Session, role Mode, sessionID string) *Session {
	if conn == nil || mux == nil {
		return nil
	}
	return &Session{
		ID:        sessionID,
		Conn:      conn,
		Mux:       mux,
		Role:      role,
		closing:   make(chan struct{}),
		startedAt: time.Now(),
	}
}

// OpenStream opens a new stream on the session.
func (s *Session) OpenStream() (net.Conn, error) {
	stream, err := s.Mux.OpenStream()
	if err != nil {
		return nil, fmt.Errorf("transport: open stream: %w", err)
	}
	return stream, nil
}

// AcceptStream blocks until a stream is accepted from the peer.
func (s *Session) AcceptStream() (net.Conn, error) {
	stream, err := s.Mux.AcceptStream()
	if err != nil {
		return nil, fmt.Errorf("transport: accept stream: %w", err)
	}
	return stream, nil
}

// Close shuts the session down exactly once, cascading to owned streams.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		close(s.closing)
	})
	if err := s.Mux.Close(); err != nil {
		return fmt.Errorf("%w: %v", ErrCloseFailed, err)
	}
	return nil
}

// Done returns a channel that closes when the session ends.
func (s *Session) Done() <-chan struct{} {
	return s.closing
}

// Uptime returns how long the session has been alive.
func (s *Session) Uptime() time.Duration {
	return time.Since(s.startedAt)
}

// HealthySince reports whether the session has been continuously alive for at
// least min, i.e. far enough along that backoff can reset.
func (s *Session) HealthySince(min time.Duration) bool {
	return s.Uptime() >= min
}

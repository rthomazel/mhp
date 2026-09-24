package transport

import (
	"fmt"
	"io"
	"net"

	"github.com/hashicorp/yamux"
)

// defaultMaxStreamBytes bounds the per-stream window size.
const defaultMaxStreamBytes = 4194304

// defaultStreamConcurrency is the yamux accept backlog. It bounds how many
// inbound streams may wait to be accepted before the peer is throttled; the
// plan fixes the relay/exit/browser concurrency at 64.
const defaultStreamConcurrency = 64

// newClientSession starts a yamux client session on an authenticated TLS conn.
func newClientSession(conn net.Conn, timing Timing) (*yamux.Session, error) {
	session, err := yamux.Client(conn, buildSessionConfig(timing))
	if err != nil {
		return nil, fmt.Errorf("transport: start client session: %w", err)
	}
	return session, nil
}

// newServerSession starts a yamux server session on an authenticated TLS conn.
func newServerSession(conn net.Conn, timing Timing) (*yamux.Session, error) {
	session, err := yamux.Server(conn, buildSessionConfig(timing))
	if err != nil {
		return nil, fmt.Errorf("transport: start server session: %w", err)
	}
	return session, nil
}

// buildSessionConfig returns the yamux config tuned from timing.
func buildSessionConfig(timing Timing) *yamux.Config {
	// AcceptBacklog is required to be positive by yamux.VerifyConfig; it is
	// also the first line of defence against unbounded stream buildup.
	return &yamux.Config{
		AcceptBacklog:          defaultStreamConcurrency,
		EnableKeepAlive:        true,
		KeepAliveInterval:      timing.KeepAliveInterval,
		ConnectionWriteTimeout: timing.PingTimeout,
		MaxStreamWindowSize:    uint32(defaultMaxStreamBytes),
		StreamOpenTimeout:      timing.StreamOpenTimeout,
		StreamCloseTimeout:     timing.DrainTimeout,
		LogOutput:              io.Discard,
	}
}

package transport

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"

	"github.com/hashicorp/yamux"
)

// defaultMaxStreamBytes bounds the per-stream window size.
const defaultMaxStreamBytes = 4194304

// defaultStreamConcurrency is the yamux accept backlog. It bounds how many
// inbound streams may wait to be accepted before the peer is throttled; the
// plan fixes the relay/exit/browser concurrency at 64.
const defaultStreamConcurrency = 64

// newClientSession starts a yamux client session on an authenticated TLS conn.
func newClientSession(conn net.Conn, timing Timing, log *slog.Logger) (*yamux.Session, error) {
	session, err := yamux.Client(conn, buildSessionConfig(timing, log))
	if err != nil {
		return nil, fmt.Errorf("transport: start client session: %w", err)
	}
	return session, nil
}

// newServerSession starts a yamux server session on an authenticated TLS conn.
func newServerSession(conn net.Conn, timing Timing, log *slog.Logger) (*yamux.Session, error) {
	session, err := yamux.Server(conn, buildSessionConfig(timing, log))
	if err != nil {
		return nil, fmt.Errorf("transport: start server session: %w", err)
	}
	return session, nil
}

// slogWriter adapts an *slog.Logger to io.Writer so yamux protocol logs
// (keepalive pings, frame diagnostics) can be routed into the project's
// structured logger. Every yamux line is emitted at Debug, so it stays
// invisible unless -debug lifts the logger level.
type slogWriter struct{ l *slog.Logger }

func (w slogWriter) Write(p []byte) (int, error) {
	msg := strings.TrimRight(string(p), "\n")
	if msg != "" {
		w.l.Debug(msg)
	}
	return len(p), nil
}

// buildSessionConfig returns the yamux config tuned from timing. Protocol logs
// are routed to log when provided; with no logger they are discarded.
func buildSessionConfig(timing Timing, log *slog.Logger) *yamux.Config {
	// AcceptBacklog is required to be positive by yamux.VerifyConfig; it is
	// also the first line of defence against unbounded stream buildup.
	output := io.Discard
	if log != nil {
		output = slogWriter{log}
	}
	return &yamux.Config{
		AcceptBacklog:          defaultStreamConcurrency,
		EnableKeepAlive:        true,
		KeepAliveInterval:      timing.KeepAliveInterval,
		ConnectionWriteTimeout: timing.PingTimeout,
		MaxStreamWindowSize:    uint32(defaultMaxStreamBytes),
		StreamOpenTimeout:      timing.StreamOpenTimeout,
		StreamCloseTimeout:     timing.DrainTimeout,
		LogOutput:              output,
	}
}

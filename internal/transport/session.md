# Types

## Session

1. ID string — the relay-issued identifier.
2. Conn net.Conn — the authenticated TLS connection backing the session.
3. Mux *yamux.Session — the multiplexer holding every stream.
4. Role Mode — the client's role (exit or proxy).
5. Closing chan struct{} — closed once when the session ends.
6. StartedAt Time — monotonic time the handshake and auth completed.

A Session is the unit a handler serves. It carries exactly one role's worth of streams.

# Functions

## newSession(conn net.Conn, mux *yamux.Session, role Mode, sessionID string) *Session

1. if conn is nil, return nil.
2. if mux is nil, return nil.
3. build a Session from conn, mux, role, sessionID, and startedAt=now.
4. return the session.

## (s *Session) OpenStream() (net.Conn, error)

1. Call Mux.OpenStream.
2. return the stream or the yamux error.

#### Errors

- **2.** if the session is closed, return the yamux error.

## (s *Session) AcceptStream() (net.Conn, error)

1. Await Mux.AcceptStream.
2. return the stream or the yamux error.

#### Errors

- **2.** if the session is closed, return the yamux error.

## (s *Session) Close() error

1. Close Mux, which cascades a close to every owned stream.
2. Close Closing exactly once, guarded by a sync.Once.
3. return the mux close error.

## (s *Session) Done() <-chan struct{}

1. return Closing.

## (s *Session) Uptime() Duration

1. return time.Since(StartedAt).

## (s *Session) HealthySince(min Duration) bool

1. return Uptime() no less than min.

# Notes

Close is idempotent under cancellation: the relay and exit handlers call Close when their work ends or the parent context is cancelled, whichever fires first.

# Constants

defaultMaxStreamBytes = 4194304, the yamux maximum per-stream window size.

defaultStreamConcurrency = 64, the yamux accept backlog bounding inbound streams before the peer is throttled; the plan sets relay/exit/browser concurrency at 64.

# Functions

## buildSessionConfig(timing Timing) *yamux.Config

1. Return a &yamux.Config with AcceptBacklog=defaultStreamConcurrency, EnableKeepAlive=true, KeepAliveInterval=timing.KeepAliveInterval, ConnectionWriteTimeout=timing.PingTimeout, MaxStreamWindowSize=defaultMaxStreamBytes, StreamOpenTimeout=timing.StreamOpenTimeout, StreamCloseTimeout=timing.DrainTimeout, LogOutput=io.Discard.

## newClientSession(conn net.Conn, timing Timing) (*yamux.Session, error)

1. Call yamux.Client(conn, buildSessionConfig(timing)).
2. return the session or the wrapped error.

#### Errors

- **1.** if the session start fails, return the wrapped error.

## newServerSession(conn net.Conn, timing Timing) (*yamux.Session, error)

1. Call yamux.Server(conn, buildSessionConfig(timing)).
2. return the session or the wrapped error.

#### Errors

- **1.** if the session start fails, return the wrapped error.

# Notes

yamux owns all framing and flow control from this point; no custom records travel on the session. Stream open is bounded externally via StreamOpenTimeout, because yamux has no native open deadline. The 64-stream concurrency cap is enforced by the session registry in the relay and exit packages, not here. AcceptBacklog doubles as the first line of defence against unbounded stream buildup.

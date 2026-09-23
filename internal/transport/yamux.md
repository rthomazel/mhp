# Constants

defaultMaxReceiveBytes = 65536, the yamux maximum received-unacked frame size.

defaultMaxStreamBytes = 4194304, the yamux maximum per-stream window size.

# Functions

## newClientSession(conn net.Conn, timing Timing) (*yamux.Session, error)

1. Build a yamux.Config with ClientMode=true, KeepAlive=timing.KeepAliveInterval, PingTimeout=timing.PingTimeout, DisableKeepalive=false, MaxReceiveBufferSize=defaultMaxReceiveBytes, MaxStreamWindowSize=defaultMaxStreamBytes.
2. Call yamux.StartSession(conn, config).
3. return the session and nil.

#### Errors

- **2.** if StartSession fails, return the wrapped error.

## newServerSession(conn net.Conn, timing Timing) (*yamux.Session, error)

1. Build a yamux.Config identically to newClientSession but with ClientMode=false.
2. Call yamux.StartSession.
3. return the session and nil.

#### Errors

- **2.** if StartSession fails, return the wrapped error.

# Notes

yamux owns all framing and flow control from this point; no custom records travel on the session. Stream open is bounded externally: the adapter sets a StreamOpenTimeout deadline on each stream right after it is opened, because yamux has no native open deadline. The 64-stream concurrency cap is enforced by the session registry in the relay and exit packages, not here.

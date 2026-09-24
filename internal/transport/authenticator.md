Orchestrates the full client-side journey: dial, handshake, hello/response, then hand the socket to yamux. It trusts the relay CA (never InsecureSkipVerify), authenticates with a bearer token, and refuses to create a session before the relay accepts.

# Vars

## ErrAuthentication = "transport: authentication failed"

Returned to the connector when the relay rejects the token; the connector backs off and retries rather than surfacing a fatal error.

## ErrDial = "transport: dial failed"

Returned when the TCP handshake fails; treated as transient.

## ErrHandshake = "transport: TLS handshake failed"

Returned when the handshake times out or fails; treated as transient.

## ErrExchange = "transport: hello exchange failed"

Returned when the hello/response exchange corrupts mid-flight.

## ErrCloseFailed = "transport: session close failed"

Returned when the underlying conn fails to close during teardown.

## ErrNilSession = "transport: nil session"

Returned when the constructed session is nil.

# Types

## Authenticator

1. Addr string — relay host:port to dial.
2. Role Mode — exit or proxy.
3. Token string — bearer to present.
4. Timing Timing — connection and session timings.
5. ServerName string — SNI the client presents and verifies.
6. RootCAs *x509.CertPool — trusted relay CA, built from the client's -ca file.
7. MinTLSVersion uint16 — pinned minimum TLS version, e.g. tls.VersionTLSxx.

Constructors take no hidden goroutines; Run owns all lifecycle.

# Functions

## (a *Authenticator) dial(ctx Context, timing Timing, dialer *net.Dialer) (net.Conn, error)

1. Build the TLSConfig from a.ServerName, a.RootCAs, and a.MinTLSVersion.
2. dialTLS(ctx, a.Addr, cfg, timing, dialer).
3. return the conn or the wrapped error.

#### Errors

- **2.** if the TCP connect fails, the error is wrapped as ErrDial.
- **2.** if the TLS handshake fails, the error is wrapped as ErrHandshake.

## (a *Authenticator) exchange(ctx Context, conn net.Conn, timing Timing) (Response, error)

1. set conn.SetDeadline(now.Add(timing.SetupTimeout)) to bound the whole exchange.
2. writeJSON(conn, Hello{Version: helloVersion, Role: a.Role, Token: a.Token}) using framing.writeJSON.
3. if sending fails, wrap as ErrExchange.
4. var response Response; err := framing.readJSON(conn, &response).
5. if receiving fails, return the wrapped error.
6. validateResponse(response).
7. if validation fails, return the validation error.
8. if response.Status differs from statusCodeOK, return ErrAuthentication naming the relay's error code.
9. clear the deadline with conn.SetDeadline(time.Time{}); the mux now owns the socket and must not inherit the setup budget.
10. return response and nil.

#### Errors

- **2.** if writing the hello fails, return ErrExchange.
- **3.** if reading the response fails, return ErrExchange.
- **5.** if the response fails validation, return the validation error.

## (a *Authenticator) authenticate(ctx Context, log *slog.Logger, dialer *net.Dialer) (*Session, error)

1. conn, err := a.dial(ctx, a.Timing, dialer).
2. if the dial fails, return the wrapped error.
3. response, err := a.exchange(ctx, conn, a.Timing).
4. if the exchange fails, close conn and return the wrapped error.
5. mux, err := newClientSession(conn, a.Timing).
6. if building the mux fails, close conn and return the wrapped error.
7. session := newSession(conn, mux, a.Role, response.SessionID).
8. if session is nil, close mux and return ErrNilSession.
9. log handshake complete with sessionID and role.
10. return session and nil.

#### Errors

- **2.** if the dial fails, return the wrapped error.
- **4.** if the exchange fails, return the wrapped error.
- **6.** if building the mux fails, return the wrapped error.
- **8.** if the session is nil, return ErrNilSession.

## (a *Authenticator) Run(ctx Context, log *slog.Logger, dialer *net.Dialer) (*Session, error)

1. Attempt authenticate.
2. if it succeeds, return the session and nil.
3. if it fails, log the error and sleep a backoff.
4. if the sleep completes before ctx is cancelled, attempt again.
5. if ctx is cancelled, return ctx.Err().

#### Errors

- **5.** if the context is cancelled, return ctx.Err().

## (a *Authenticator) backoff(attempt int) Duration

1. Compute base = min_backoff << attempt, clamped to max_backoff.
2. Add a constant random jitter bounded by min_backoff * jitter_ratio.
3. Clamp the result to max_backoff.
4. return base.

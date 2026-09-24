Relay-side accept→verify→respond→mux-start. The relay presents its leaf and verifies no one, accepting any client that presents a valid bearer.

# Vars

## ErrEmptyBearer = "transport: empty bearer"

Returned when a presented token is blank.

## ErrBadRole = "transport: unexpected role"

Returned when Hello.Role is neither exit nor proxy.

## ErrWrongToken = "transport: wrong token"

Returned when the presented token does not match the relay's expected bearer for the role.

## ErrSendFailure = "transport: send failure"

Returned when the relay cannot write the verdict frame.

## ErrRecvFailure = "transport: receive failure"

Returned when the relay cannot read the hello frame.

## ErrListenTLS = "transport: TLS listen failed"

Returned when the TLS handshake on the accepted socket fails.

## ErrNilSession = "transport: nil session"

Returned when the constructed session is nil.

# Types

## Handshake

1. TLSConfig TLSConfig — relay leaf and private key; absent on clients.
2. Timing Timing — connection timings.
3. Logger *slog.Logger — diagnostics.
4. Verifier Verifier — trusted role→bearer map.

Constructors take no hidden goroutines; Run owns all lifecycle.

## Verifier

1. ExpectedExits map[Mode]string — role→bearer map the relay trusts. Built once at startup from config; read-only afterwards.

# Functions

## (h *Handshake) handshake(ctx Context, raw net.Conn) (*Session, error)

1. conn, err := listenTLS(ctx, raw, h.TLSConfig, h.Timing).
2. if listening fails, return ErrListenTLS.
3. read hello := framing.readJSON(conn).
4. if reading fails, return ErrRecvFailure.
5. validateHello(hello).
6. if validation fails, return the wrapped error.
7. err := h.Verifier.verify(hello.Role, hello.Token).
8. if verification fails, return the wrapped error.
9. build Response{Version: helloVersion, Status: statusCodeOK, SessionID: newSessionID()}.
10. writeJSON(conn, resp).
11. if writing fails, return ErrSendFailure.
12. mux, err := newServerSession(conn, h.Timing).
13. if building the mux fails, return the wrapped error.
14. session := newSession(conn, mux, hello.Role, sessionID).
15. if session is nil, return ErrNilSession.
16. log handshake complete with sessionID and role.
17. return session and nil.

#### Errors

- **2.** if listening fails, return ErrListenTLS.
- **4.** if reading fails, return ErrRecvFailure.
- **6.** if validation fails, return the wrapped error.
- **8.** if verification fails, return the wrapped error.
- **11.** if writing fails, return ErrSendFailure.
- **13.** if building the mux fails, return the wrapped error.
- **15.** if the session is nil, return ErrNilSession.

## (h *Handshake) Run(ctx Context, ln net.Listener) (*Session, error)

1. accept a conn from ln.
2. if accept fails and ctx is cancelled, return nil.
3. if accept fails for any other reason, return the wrapped error.
4. attempt handshake on the accepted conn.
5. if handshake fails, close the raw conn, log the failure, and accept the next.
6. return the session and nil.

#### Errors

- **3.** if accept fails for a reason other than cancellation, return the wrapped error.

## (v *Verifier) verify(role Mode, token string) error

1. if role is neither exit nor proxy, return ErrBadRole.
2. if token is empty, return ErrEmptyBearer.
3. if expected, ok := v.ExpectedExits[role]; !ok, return ErrWrongToken.
4. if token != expected, return ErrWrongToken.
5. return nil.

#### Errors

- **1.** if role is invalid, return ErrBadRole.
- **2.** if token is empty, return ErrEmptyBearer.
- **3.** if the relay has no expectation for the role, return ErrWrongToken.
- **4.** if the token does not match, return ErrWrongToken.

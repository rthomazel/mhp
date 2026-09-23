# Vars

## ErrEmptyBearer = "transport: empty bearer"

Returned when a presented token is blank.

## ErrBadRole = "transport: unexpected role"

Returned when Hello.Role is neither exit nor proxy.

## ErrWrongToken = "transport: wrong token"

Returned when the presented token does not match the relay's expected bearer for the role.

## ErrBadVersion = "transport: unexpected protocol version"

Returned when the presented version is not helloVersion.

## ErrMalformedHello = "transport: malformed hello"

Returned when the incoming record is not valid JSON.

## ErrSendFailure = "transport: send failure"

Returned when the relay cannot write the verdict frame.

## ErrRecvFailure = "transport: receive failure"

Returned when the relay cannot read the hello frame.

## ErrListenTLS = "transport: TLS listen failed"

Returned when the TLS handshake on the accepted socket fails.

## ErrNilConn = "transport: nil connection"

Returned when the mux is nil.

## ErrNilSession = "transport: nil session"

Returned when the constructed session is nil.

# Types

## Verifier

1. ExpectedExits map[Mode]string — role→bearer map the relay trusts. Built once at startup from config; read-only afterwards.

# Functions

## (v *Verifier) verify(role Mode, token string, timing Timing) error

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

## (v *Verifier) handshake(ctx Context, raw net.Conn, role Mode, timing Timing, dialer *net.Dialer) (*Session, error)

1. conn, err := listenTLS(raw, role, timing, dialer).
2. if listening fails, return the wrapped error.
3. read hello := framing.readJSON(ctx, conn).
4. if reading fails, return the wrapped error.
5. validateHello(hello).
6. if validation fails, return the wrapped error.
7. err := v.verify(hello.Role, hello.Token, timing).
8. if verification fails, return the wrapped error.
9. respond(conn, hello.Role, true, newSessionID()).
10. if responding fails, return ErrSendFailure.
11. mux, err := newServerSession(conn, timing).
12. if building the mux fails, return the wrapped error.
13. session := newSession(conn, mux, hello.Role, sessionID).
14. if session is nil, return ErrNilSession.
15. log handshake complete with sessionID and role.
16. return session and nil.

#### Errors

- **2.** if listening fails, return ErrListenTLS.
- **4.** if reading fails, return ErrRecvFailure.
- **6.** if validation fails, return the wrapped error.
- **8.** if verification fails, return the wrapped error.
- **10.** if responding fails, return ErrSendFailure.
- **12.** if building the mux fails, return the wrapped error.
- **14.** if the session is nil, return ErrNilSession.

## respond(conn net.Conn, role Mode, ok bool, sessionID string) error

1. status := statusCodeError.
2. if ok, status := statusCodeOK.
3. resp := Response{Version: helloVersion, Status: status, SessionID: sessionID, ErrorCode: errorCode}.
4. writeJSON(conn, resp).
5. if writing fails, return ErrSendFailure.
6. return nil.

#### Errors

- **4.** if writing fails, return ErrSendFailure.

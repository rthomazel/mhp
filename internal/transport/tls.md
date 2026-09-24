Building and verifying TLS connections. Clients trust only the relay CA and present no client certificate; the relay presents its leaf and verifies no one. netdiag's InsecureSkipVerify is never set.

# Constants

DefaultMinTLSVersion = tls.VersionTLS13, the pinned minimum TLS version for both client and relay.

# Vars

## ErrUntrustedCA = "transport: untrusted CA"

Returned when the client's -ca file cannot be parsed into a trust pool.

# Types

## TLSConfig

1. ServerName string — SNI the client presents and verifies.
2. RootCAs *x509.CertPool — trusted relay CA, built from the client's -ca file.
3. Certificate tls.Certificate — relay leaf and key; absent on clients.
4. MinTLSVersion Version — pinned to the newest stable TLS version Go ships.

# Functions

## LoadTrustPool(path string) (*x509.CertPool, error)

1. Read the PEM file at path.
2. Append the contents to a fresh x509.CertPool.
3. if parsing fails, return ErrUntrustedCA.
4. return the pool and nil.

#### Errors

- **3.** if parsing fails, return ErrUntrustedCA.

## dialTLS(ctx Context, addr string, cfg TLSConfig, timing Timing, dialer *net.Dialer) (net.Conn, error)

1. Dial addr with a deadline of DialTimeout.
2. if the dial fails, return the wrapped error.
3. Upgrade the connection to TLS using cfg, presenting ServerName and trusting RootCAs.
4. if the handshake fails, return the wrapped error.
5. clear the deadline so the mux owns the socket and inherits no setup budget.
6. return the TLS conn and nil.

#### Errors

- **2.** if the dial fails, return the wrapped error.
- **4.** if the handshake fails, return the wrapped error.

## listenTLS(ctx Context, raw net.Conn, cfg TLSConfig, timing Timing) (net.Conn, error)

1. Wrap raw in a *tls.Conn using cfg.
2. set a HandshakeTimeout.
3. Complete the TLS handshake server side.
4. if the handshake fails, return the wrapped error.
5. return the TLS conn and nil.

#### Errors

- **4.** if the handshake fails, return the wrapped error.

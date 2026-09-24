package transport

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"time"
)

// DefaultMinTLSVersion is the pinned minimum TLS version for both client and
// relay: the newest stable TLS version Go ships.
const DefaultMinTLSVersion = tls.VersionTLS13

// ErrDial is returned when the TCP handshake fails.
var ErrDial = errors.New("transport: dial failed")

// ErrHandshake is returned when the TLS handshake fails.
var ErrHandshake = errors.New("transport: TLS handshake failed")

// ErrUntrustedCA is returned when the client's -ca file cannot be parsed into a
// trust pool.
var ErrUntrustedCA = errors.New("transport: untrusted CA")

// TLSConfig mirrors the fields Go's crypto/tls.Config needs for MHP, grouped so
// callers can hand the whole bundle around without leaking raw pointers.
type TLSConfig struct {
	// ServerName is the SNI the client presents and verifies.
	ServerName string
	// RootCAs is the trusted relay CA pool, absent on the relay.
	RootCAs *x509.CertPool
	// Certificate is the relay's leaf+key on the server side, absent on clients.
	Certificate tls.Certificate
	// MinTLSVersion pins the floor on both ends (tls.VersionTLSxx).
	MinTLSVersion uint16
}

// dialTLS opens a TCP connection to addr, upgrades it to TLS presenting
// ServerName and trusting RootCAs, and bounds the handshake by timing.
func dialTLS(ctx context.Context, addr string, cfg TLSConfig, timing Timing, dialer *net.Dialer) (net.Conn, error) {
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrDial, addr, err)
	}
	if err := conn.SetDeadline(time.Now().Add(timing.DialTimeout)); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: %v", ErrDial, err)
	}
	tlsCfg := &tls.Config{
		ServerName:         cfg.ServerName,
		RootCAs:            cfg.RootCAs,
		MinVersion:         cfg.MinTLSVersion,
		InsecureSkipVerify: false,
	}
	tlsConn := tls.Client(conn, tlsCfg)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: %v", ErrHandshake, err)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: %v", ErrDial, err)
	}
	return tlsConn, nil
}

// listenTLS upgrades raw to a TLS server connection and completes the handshake
// server side, bounding it by timing.
func listenTLS(ctx context.Context, raw net.Conn, cfg TLSConfig, timing Timing) (net.Conn, error) {
	if err := raw.SetDeadline(time.Now().Add(timing.HandshakeTimeout)); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("transport: set handshake deadline: %w", err)
	}
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cfg.Certificate},
		MinVersion:   cfg.MinTLSVersion,
	}
	tlsConn := tls.Server(raw, tlsCfg)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("transport: TLS handshake: %w", err)
	}
	if err := raw.SetDeadline(time.Time{}); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("transport: clear deadline: %w", err)
	}
	return tlsConn, nil
}

// LoadTrustPool parses a PEM file of trusted CA certificates into a cert pool.
func LoadTrustPool(path string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("transport: read CA file %q: %w", path, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("%w: %s", ErrUntrustedCA, path)
	}
	return pool, nil
}

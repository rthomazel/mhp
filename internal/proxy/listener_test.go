package proxy

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/rthomazel/mhp/internal/config"
	"github.com/rthomazel/mhp/internal/stream"
	"github.com/rthomazel/mhp/internal/transport"
	socks5 "github.com/things-go/go-socks5"
)

func TestRequestConnCloseWriteClosesLocalConnection(t *testing.T) {
	client, local := net.Pipe()
	defer func() { _ = client.Close() }()
	request := &requestConn{Conn: local, reader: local}

	if err := stream.CloseWrite(request); err != nil {
		t.Fatalf("close relay-facing write side: %v", err)
	}

	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("local connection remained open after relay stream close")
	}
}

func TestForwardRequestNegotiatesExitNoAuth(t *testing.T) {
	browser, exit := net.Pipe()
	defer func() { _ = browser.Close() }()
	defer func() { _ = exit.Close() }()
	_ = browser.SetDeadline(time.Now().Add(time.Second))
	_ = exit.SetDeadline(time.Now().Add(time.Second))

	connectRequest := []byte{0x05, 0x01, 0x00, 0x01, 1, 1, 1, 1, 0, 80}
	request, err := socks5.ParseRequest(bytes.NewReader(connectRequest))
	if err != nil {
		t.Fatalf("parse test CONNECT request: %v", err)
	}

	serverErr := make(chan error, 1)
	go func() {
		greeting := make([]byte, 3)
		if _, err := io.ReadFull(exit, greeting); err != nil {
			serverErr <- err
			return
		}
		if !bytes.Equal(greeting, []byte{0x05, 0x01, 0x00}) {
			serverErr <- errors.New("unexpected SOCKS5 greeting")
			return
		}
		if _, err := exit.Write([]byte{0x05, 0x00}); err != nil {
			serverErr <- err
			return
		}
		got := make([]byte, len(connectRequest))
		if _, err := io.ReadFull(exit, got); err != nil {
			serverErr <- err
			return
		}
		if !bytes.Equal(got, connectRequest) {
			serverErr <- errors.New("forwarded CONNECT request differs")
			return
		}
		serverErr <- nil
	}()

	if err := forwardRequest(browser, request); err != nil {
		t.Fatalf("forward request: %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

func TestForwardRequestRejectsUnsupportedExitAuth(t *testing.T) {
	browser, exit := net.Pipe()
	defer func() { _ = browser.Close() }()
	defer func() { _ = exit.Close() }()
	_ = browser.SetDeadline(time.Now().Add(time.Second))
	_ = exit.SetDeadline(time.Now().Add(time.Second))

	request, err := socks5.ParseRequest(bytes.NewReader([]byte{0x05, 0x01, 0x00, 0x01, 1, 1, 1, 1, 0, 80}))
	if err != nil {
		t.Fatalf("parse test CONNECT request: %v", err)
	}

	serverErr := make(chan error, 1)
	go func() {
		greeting := make([]byte, 3)
		_, err := io.ReadFull(exit, greeting)
		if err == nil {
			_, err = exit.Write([]byte{0x05, 0xff})
		}
		serverErr <- err
	}()

	if err := forwardRequest(browser, request); err == nil {
		t.Fatal("forwardRequest succeeded when exit rejected no-auth")
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

func TestAuthMethods(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	conn := transport.NewConnector(transport.Authenticator{}, quietLogger(), nil)
	listener := New(ln, conn, quietLogger(), "alice", "secret")
	methods := listener.authMethods()
	if len(methods) != 1 {
		t.Fatalf("auth methods = %d, want 1", len(methods))
	}
	if methods[0].GetCode() != 0x02 {
		t.Fatalf("auth method = %d, want username/password", methods[0].GetCode())
	}
}

func testTiming() transport.Timing {
	t := transport.DefaultTiming
	t.SetupTimeout = 2 * time.Second
	t.DialTimeout = 2 * time.Second
	t.HandshakeTimeout = 2 * time.Second
	t.MinBackoff = 10 * time.Millisecond
	t.MaxBackoff = 50 * time.Millisecond
	return t
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// selfSigned builds an in-memory TLS cert for localhost plus a pool trusting it.
func selfSigned(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("serial: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return cert, pool
}

// relayStandUp brings up a bare relay Handshake. It authenticates clients and
// negotiates yamux — enough to let the connector publish a session — without
// bridging, which is all the proxy test needs.
func relayStandUp(t *testing.T, role config.Mode, token string) (addr string, pool *x509.CertPool, stop func()) {
	t.Helper()
	cert, pool := selfSigned(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("relay listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	hs := &transport.Handshake{
		TLSConfig: transport.TLSConfig{Certificate: cert, MinTLSVersion: transport.DefaultMinTLSVersion},
		Timing:    testTiming(),
		Logger:    quietLogger(),
		Verifier:  transport.Verifier{ExpectedExits: map[transport.Mode]string{role: token}},
	}
	go func() { _, _ = hs.Run(ctx, ln) }()
	return ln.Addr().String(), pool, func() {
		cancel()
		_ = ln.Close()
	}
}

// waitForSession polls the connector until it publishes a linked session.
func waitForSession(t *testing.T, conn *transport.Connector) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		if _, ok := conn.Snapshot(); ok {
			return
		}
		select {
		case <-deadline:
			t.Fatal("connector never published a session")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// TestProxyWithSessionKeepsConnectionOpen drives the bridging path: with a
// published relay session, a browser connection is bridged and stays open (a
// bounded read times out) rather than being closed by the no-session fallback.
func TestProxyWithSessionKeepsConnectionOpen(t *testing.T) {
	addr, pool, stop := relayStandUp(t, config.ModeProxy, "tok")
	defer stop()

	auth := transport.Authenticator{
		Addr: addr, Role: config.ModeProxy, Token: "tok",
		Timing: testTiming(), ServerName: "localhost", RootCAs: pool,
		MinTLSVersion: transport.DefaultMinTLSVersion,
	}
	conn := transport.NewConnector(auth, quietLogger(), &net.Dialer{})
	connCtx, connCancel := context.WithCancel(context.Background())
	defer connCancel()
	go func() { _ = conn.Run(connCtx) }()
	waitForSession(t, conn)

	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("proxy listen: %v", err)
	}
	defer func() { _ = proxyLn.Close() }()

	ln := New(proxyLn, conn, quietLogger())
	if got := ln.ListenAddr(); got == nil {
		t.Fatal("ListenAddr returned nil")
	}

	runCtx, runCancel := context.WithCancel(context.Background())
	defer runCancel()
	runDone := make(chan error, 1)
	go func() { runDone <- ln.Run(runCtx) }()

	client, err := net.Dial("tcp", proxyLn.Addr().String())
	if err != nil {
		t.Fatalf("browser dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	if _, err := client.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = client.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected open (bridged) connection to time out on read")
	}

	runCancel()
	select {
	case err := <-runDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

// TestProxyNoSessionClosesPromptly verifies that when the connector has no
// published session, a browser connection is closed promptly rather than held.
func TestProxyNoSessionClosesPromptly(t *testing.T) {
	auth := transport.Authenticator{
		Addr: "127.0.0.1:1", Role: config.ModeProxy, Token: "tok",
		Timing: testTiming(), ServerName: "localhost",
		RootCAs: x509.NewCertPool(), MinTLSVersion: transport.DefaultMinTLSVersion,
	}
	conn := transport.NewConnector(auth, quietLogger(), &net.Dialer{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = conn.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	if _, ok := conn.Snapshot(); ok {
		t.Fatal("connector unexpectedly published a session")
	}

	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("proxy listen: %v", err)
	}
	defer func() { _ = proxyLn.Close() }()
	ln := New(proxyLn, conn, quietLogger())

	runCtx, runCancel := context.WithCancel(context.Background())
	defer runCancel()
	runDone := make(chan error, 1)
	go func() { runDone <- ln.Run(runCtx) }()

	client, err := net.Dial("tcp", proxyLn.Addr().String())
	if err != nil {
		t.Fatalf("browser dial: %v", err)
	}
	defer func() { _ = client.Close() }()

	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("expected closed connection, got no error")
	}

	runCancel()
	select {
	case err := <-runDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

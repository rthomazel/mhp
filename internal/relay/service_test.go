package relay

import (
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
	"github.com/rthomazel/mhp/internal/transport"
)

// testTiming shrinks the transport timings so handshake-driven tests finish
// quickly instead of waiting on the production 15s+ budgets.
func testTiming() transport.Timing {
	t := transport.DefaultTiming
	t.SetupTimeout = 2 * time.Second
	t.DialTimeout = 2 * time.Second
	t.HandshakeTimeout = 2 * time.Second
	t.MinBackoff = 10 * time.Millisecond
	t.MaxBackoff = 50 * time.Millisecond
	return t
}

// quietLogger returns a slog.Logger that discards output.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// selfSigned builds an in-memory TLS cert for localhost plus a pool that trusts
// it. transport's own helpers are unexported, so the relay tests reuse this.
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

// TestServiceRunServesProxyClient drives the whole relay acceptance path: the
// service owns a listener, a proxy client authenticates through it, the service
// registers the session, and Run returns the parent's error on cancellation.
func TestServiceRunServesProxyClient(t *testing.T) {
	cert, pool := selfSigned(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	addr := ln.Addr().String()

	svc := NewService(&transport.Handshake{
		TLSConfig: transport.TLSConfig{Certificate: cert, MinTLSVersion: transport.DefaultMinTLSVersion},
		Timing:    testTiming(),
		Logger:    quietLogger(),
		Verifier:  transport.Verifier{ExpectedExits: map[transport.Mode]string{config.ModeProxy: "tok"}},
	}, quietLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx, ln) }()

	// Drive a client through to the relay so the service registers it.
	clientAuth := transport.Authenticator{
		Addr: addr, Role: config.ModeProxy, Token: "tok",
		Timing: testTiming(), ServerName: "localhost", RootCAs: pool,
		MinTLSVersion: transport.DefaultMinTLSVersion,
	}
	conn := transport.NewConnector(clientAuth, quietLogger(), &net.Dialer{})
	connCtx, connCancel := context.WithCancel(context.Background())
	defer connCancel()
	connDone := make(chan error, 1)
	go func() { connDone <- conn.Run(connCtx) }()
	// Wait until the connector publishes a linked session, proving the relay
	// accepted and registered the client.
	deadline := time.After(3 * time.Second)
	for {
		if _, ok := conn.Snapshot(); ok {
			break
		}
		select {
		case <-deadline:
			t.Fatal("client connector never published a session")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	// Give the service a moment to register, then cancel.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

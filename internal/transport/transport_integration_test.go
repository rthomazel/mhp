package transport

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/rthomazel/mhp/internal/config"
)

// newRelay spins up a relay Handshake bound to an ephemeral listener and returns
// the listener address plus a stop function that cancels the relay's context.
func newRelay(t *testing.T, role config.Mode, token string) (addr string, pool *x509.CertPool, stop func()) {
	t.Helper()
	cert, pool, _ := generateSelfSigned(t, "localhost")
	ln := freeListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	hs := &Handshake{
		TLSConfig: TLSConfig{Certificate: cert, MinTLSVersion: DefaultMinTLSVersion},
		Timing:    fastTiming(),
		Logger:    quietLogger(),
		Verifier:  Verifier{ExpectedExits: map[Mode]string{role: token}},
	}
	go func() {
		_, _ = hs.Run(ctx, ln)
	}()
	return ln.Addr().String(), pool, func() {
		cancel()
	}
}

func fastTiming() Timing {
	t := DefaultTiming
	t.SetupTimeout = 2 * time.Second
	t.DialTimeout = 2 * time.Second
	t.HandshakeTimeout = 2 * time.Second
	t.MinBackoff = 10 * time.Millisecond
	t.MaxBackoff = 50 * time.Millisecond
	return t
}

func TestHandshakeHappyPath(t *testing.T) {
	addr, pool, stop := newRelay(t, config.ModeProxy, "proxy-token")
	defer stop()

	a := &Authenticator{
		Addr: addr, Role: config.ModeProxy, Token: "proxy-token",
		Timing: fastTiming(), ServerName: "localhost", RootCAs: pool,
		MinTLSVersion: DefaultMinTLSVersion,
	}
	sess, err := a.authenticate(context.Background(), quietLogger(), &net.Dialer{})
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if sess.Role != config.ModeProxy {
		t.Fatalf("session role = %v, want proxy", sess.Role)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestHandshakeWrongToken(t *testing.T) {
	addr, pool, stop := newRelay(t, config.ModeProxy, "correct-token")
	defer stop()

	a := &Authenticator{
		Addr: addr, Role: config.ModeProxy, Token: "wrong-token",
		Timing: fastTiming(), ServerName: "localhost", RootCAs: pool,
		MinTLSVersion: DefaultMinTLSVersion,
	}
	sess, err := a.authenticate(context.Background(), quietLogger(), &net.Dialer{})
	if !errors.Is(err, ErrAuthentication) {
		t.Fatalf("want ErrAuthentication, got %v", err)
	}
	if sess != nil {
		// A rejected handshake must not yield a session; refuse to register mux.
		_ = sess.Close()
		t.Fatal("want nil session on rejected handshake")
	}
}

func TestHandshakeUntrustedCA(t *testing.T) {
	addr, pool, stop := newRelay(t, config.ModeProxy, "token")
	defer stop()

	a := &Authenticator{
		Addr: addr, Role: config.ModeProxy, Token: "token",
		Timing: fastTiming(), ServerName: "localhost", RootCAs: pool,
		MinTLSVersion: DefaultMinTLSVersion,
	}
	// Swap in a hostile pool by re-pointing after building the Authenticator.
	otherPool := x509.NewCertPool()
	a.RootCAs = otherPool
	_, err := a.authenticate(context.Background(), quietLogger(), &net.Dialer{})
	if err == nil {
		t.Fatal("expected TLS failure against untrusted CA")
	}
}

func TestRunCancellation(t *testing.T) {
	_, pool, _ := generateSelfSigned(t, "localhost")
	a := &Authenticator{
		Addr: "127.0.0.1:1", Role: config.ModeProxy, Token: "token",
		Timing: fastTiming(), ServerName: "localhost", RootCAs: pool,
		MinTLSVersion: DefaultMinTLSVersion,
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := a.Run(ctx, quietLogger(), &net.Dialer{})
		result <- err
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

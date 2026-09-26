package exit

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	socks5 "github.com/things-go/go-socks5"
)

// TestResolvePropagatesBaseCancellation proves the resolver derives its
// deadline from the base (session) context rather than the library's
// context.Background. When the base is cancelled, Resolve must fail fast with
// the base error, not succeed or hang.
func TestResolvePropagatesBaseCancellation(t *testing.T) {
	base, cancel := context.WithCancel(context.Background())
	defer cancel()

	r := newBoundedResolver(base, 16, 5*time.Second, nil)

	// Swap the resolve seam so it would succeed if it were ever called,
	// letting us isolate the base-cancellation path.
	r.resolve = func(ctx context.Context, name string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}}, nil
	}

	cancel()

	_, _, err := r.Resolve(context.Background(), "example.invalid")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// TestResolveUsesInjectedSeam proves a resolved IP flows through unchanged and
// that the returned context is the caller's ctx (not the library's
// Background), satisfying the "do not assume library ctx is cancellable" rule.
func TestResolveUsesInjectedSeam(t *testing.T) {
	r := newBoundedResolver(context.Background(), 16, 5*time.Second, nil)
	r.resolve = func(ctx context.Context, name string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}

	callCtx := context.Background()
	retCtx, ip, err := r.Resolve(callCtx, "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if retCtx != callCtx {
		t.Fatal("returned context must be the caller's context")
	}
	if ip.String() != "93.184.216.34" {
		t.Fatalf("unexpected IP: %v", ip)
	}
}

// TestResolveTimeoutBounding proves the resolver's timeout is honored even when
// the injected resolve seam blocks.
func TestResolveTimeoutBounding(t *testing.T) {
	base, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	r := newBoundedResolver(base, 16, 100*time.Millisecond, nil)
	r.resolve = func(ctx context.Context, name string) ([]net.IPAddr, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
			return nil, nil
		}
	}

	start := time.Now()
	_, _, err := r.Resolve(context.Background(), "hang.example")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error from blocking lookup")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("lookup exceeded reasonable bound: %v", elapsed)
	}
}

// TestResolveConcurrentCap proves the limiter bounds concurrent lookups so a
// flood of hostnames cannot spawn unbounded goroutines.
func TestResolveConcurrentCap(t *testing.T) {
	r := newBoundedResolver(context.Background(), 2, 5*time.Second, nil)

	var active atomic.Int32
	var peak atomic.Int32
	r.resolve = func(ctx context.Context, name string) ([]net.IPAddr, error) {
		cur := active.Add(1)
		for {
			curPeak := peak.Load()
			if cur <= curPeak || peak.CompareAndSwap(curPeak, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		active.Add(-1)
		return []net.IPAddr{{IP: net.ParseIP("1.2.3.4")}}, nil
	}

	// Fire 8 concurrent Resolves; only 2 may be in resolve at once.
	const n = 8
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			_, _, err := r.Resolve(context.Background(), "x.example")
			errCh <- err
		}()
	}

	for i := 0; i < n; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("lookup %d failed: %v", i, err)
		}
	}
	if peak.Load() > 2 {
		t.Fatalf("resolved concurrently: %d, cap is 2", peak.Load())
	}
	if peak.Load() < 1 {
		t.Fatal("no lookups ran")
	}
}

// TestResolveEmptyResult returns net.ErrClosed for an empty resolution.
func TestResolveEmptyResult(t *testing.T) {
	r := newBoundedResolver(context.Background(), 16, 5*time.Second, nil)
	r.resolve = func(ctx context.Context, name string) ([]net.IPAddr, error) {
		return nil, nil
	}
	_, ip, err := r.Resolve(context.Background(), "empty.example")
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("expected net.ErrClosed, got %v", err)
	}
	if ip != nil {
		t.Fatalf("expected nil IP, got %v", ip)
	}
}

// TestResolverImplementsContract proves the resolver satisfies the library's
// NameResolver interface.
func TestResolverImplementsContract(t *testing.T) {
	var _ socks5.NameResolver = (*boundedResolver)(nil)
}

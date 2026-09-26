// Package exit hosts the SOCKS5 server that terminates the browser's SOCKS5
// requests arriving over relay streams. The proxy pipes bytes untouched; the
// exit is the only place MHP interprets SOCKS5, resolves hostnames, validates
// destinations against the Internet-only policy, and dials them — so traffic
// genuinely leaves from the exit, not from the relay or the proxy.
package exit

import (
	"context"
	"log/slog"
	"net"
	"time"
)

// boundedResolver wraps net.Resolver.LookupIPAddr so the exit can resolve
// browser-supplied hostnames without either stalling on the library's
// context.Background() or exhausting resources under flood. Concurrency is
// capped by limiter, and every lookup dies at the shared base deadline so a
// hung session unblocks in-flight resolutions too.
//
// The library calls Resolve with context.Background(); the returned context is
// what flows to the connect handle, so we return the caller's ctx unchanged and
// drive all timing from base instead.
type boundedResolver struct {
	// base is the shared deadline/cancellation the exit imposes on setup.
	// It is typically the session's setup context: it dies when the session
	// ends or the setup budget elapses, whichever first.
	base context.Context
	// limiter bounds how many lookups run concurrently.
	limiter chan struct{}
	// timeout bounds a single lookup regardless of base.
	timeout time.Duration
	logger  *slog.Logger
	// resolver is the underlying name source. Tests may swap it to a stub;
	// production uses the standard net.Resolver.
	resolver net.Resolver
	// resolve performs the actual lookup. It defaults to the embedded
	// net.Resolver but can be swapped in tests to block, count, or fail
	// deterministically.
	resolve func(ctx context.Context, name string) ([]net.IPAddr, error)
}

func newBoundedResolver(base context.Context, bound int, timeout time.Duration, logger *slog.Logger) *boundedResolver {
	if bound < 1 {
		bound = 1
	}
	if logger == nil {
		logger = slog.Default()
	}
	r := &boundedResolver{
		base:    base,
		limiter: make(chan struct{}, bound),
		timeout: timeout,
		logger:  logger,
	}
	r.resolve = r.lookup
	return r
}

// lookup resolves via the standard net.Resolver, honoring the base deadline
// and timeout.
func (r *boundedResolver) lookup(ctx context.Context, name string) ([]net.IPAddr, error) {
	lookup, cancel := context.WithTimeout(r.base, r.timeout)
	defer cancel()
	return r.resolver.LookupIPAddr(lookup, name)
}

// Resolve performs a single bounded lookup. It blocks briefly waiting for a
// slot so a flood of hostnames cannot spawn unbounded goroutines, then resolves
// with a tight timeout derived from the shared base.
func (r *boundedResolver) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	// Honor base cancellation first: if the session is gone, never acquire a
	// slot or perform a lookup, regardless of how much room the limiter had.
	if err := r.base.Err(); err != nil {
		return ctx, nil, err
	}
	select {
	case r.limiter <- struct{}{}:
		defer func() { <-r.limiter }()
	case <-r.base.Done():
		return ctx, nil, r.base.Err()
	}

	addrs, err := r.resolve(ctx, name)
	if err != nil {
		r.logger.Debug("exit: dns lookup failed", "name", name, "error", err)
		return ctx, nil, err
	}
	if len(addrs) == 0 {
		return ctx, nil, net.ErrClosed
	}
	return ctx, addrs[0].IP, nil
}

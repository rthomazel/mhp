// Package stream provides the primitives that bridge a yamux stream to the
// net.Conn world: a net.Addr the mux hands back no address with, and a
// cancellation-aware duplex copy that moves bytes between a pair of net.Conns.
//
// Duplex copies bytes in both directions. Each direction is an independent
// io.Copy loop: on a clean EOF it stops reading that side only, leaving the
// opposite side fully open so response bytes can keep flowing. Only a cancelled
// context or a fatal error escalates to a forced close of both sides, which
// unblocks a stalled copy and joins every goroutine before Run returns.
package stream

import (
	"context"
	"io"
	"net"
	"sync"
	"time"
)

// Duplex copies bytes between two net.Conns in both directions.
type Duplex struct {
	inbound  net.Conn
	outbound net.Conn
}

// NewDuplex pairs the inbound and outbound conns Duplex bridges.
func NewDuplex(inbound, outbound net.Conn) *Duplex {
	return &Duplex{inbound: inbound, outbound: outbound}
}

// Run performs a full bidirectional copy between inbound and outbound. It
// returns nil when both directions finish cleanly. If the context is cancelled,
// or either direction returns a fatal error, Run applies a past deadline to
// both conns and closes them to unblock the still-running copy goroutine, then
// joins both goroutines and returns the triggering error.
func (d *Duplex) Run(ctx context.Context) error {
	var copyDone sync.WaitGroup
	copyDone.Add(2)

	// results is written by the copy goroutines and only read after
	// copyDone.Wait(), so the join establishes the happens-before relationship
	// without any lock. A nil err means the direction shut down cleanly.
	type result struct {
		err error
	}
	results := make([]result, 2)

	// A single escalation signal wakes Run when any direction hits a real
	// error. Buffered capacity of 1 lets a second goroutine signal drop
	// harmlessly; the coordinator only needs one nudge.
	escalation := make(chan struct{}, 1)

	copier := func(from, to net.Conn, i int) {
		defer copyDone.Done()
		res := result{err: d.copyDirection(from, to)}
		results[i] = res
		// Only a genuine error (not clean EOF) forces a full teardown.
		if res.err != nil {
			select {
			case escalation <- struct{}{}:
			default:
			}
		}
	}

	doneCh := make(chan struct{})
	go func() {
		copyDone.Wait()
		close(doneCh)
	}()

	go copier(d.inbound, d.outbound, 0)
	go copier(d.outbound, d.inbound, 1)

	select {
	case <-doneCh:
		return nil
	case <-ctx.Done():
		d.forceClose()
		copyDone.Wait()
		return ctx.Err()
	case <-escalation:
		d.forceClose()
		copyDone.Wait()
		return firstErr(results[0].err, results[1].err)
	}
}

// copyDirection drains from into to. On a clean EOF or nil it returns nil,
// leaving the opposite direction free to finish draining. Any other error
// propagates to the caller and forces a full teardown of both directions.
func (d *Duplex) copyDirection(from, to net.Conn) error {
	// io.Copy returns io.EOF when the source is half-closed. Treating that as
	// a clean end lets the opposite direction keep draining any bytes still
	// arriving, which is what preserves SOCKS response bytes after the request
	// side finishes.
	_, err := io.Copy(to, from)
	if err == nil || err == io.EOF {
		return nil
	}
	return err
}

// forceClose stamps a past deadline on both conns so any blocked Read or
// Write returns immediately, then closes both. This is what turns an ordinary
// half-close into a terminal reset for a stalled copy.
func (d *Duplex) forceClose() {
	deadline := time.Now().Add(-time.Second)
	_ = d.inbound.SetDeadline(deadline)
	_ = d.outbound.SetDeadline(deadline)
	_ = d.inbound.Close()
	_ = d.outbound.Close()
}

// firstErr returns the first non-nil error, or nil if none did.
func firstErr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

package stream

import (
	"net"
	"time"

	"github.com/hashicorp/yamux"
)

// Adapter adapts a yamux stream to the net.Conn contract the rest of MHP
// expects. yamux streams already implement Read, Write, Close and the
// deadline setters, but they lack addresses and collapse the half-close and
// full-close distinctions into a single Close. The adapter supplies a stable
// address pair and exposes the FIN/full-close split explicitly so Duplex can
// honour the SOCKS5 half-close contract.
type Adapter struct {
	*yamux.Stream

	// remote is the address of the peer at the far end of the stream.
	remote net.Addr
	// local is the address of this endpoint.
	local net.Addr
}

// NewAdapter wraps a yamux stream, tagging it with the addresses of its two
// endpoints. Either address may be nil until known.
func NewAdapter(stream *yamux.Stream, local, remote net.Addr) *Adapter {
	return &Adapter{Stream: stream, local: local, remote: remote}
}

// RemoteAddr implements net.Addr, reporting the stream's peer.
func (a *Adapter) RemoteAddr() net.Addr { return a.remote }

// LocalAddr implements net.Addr, reporting this endpoint.
func (a *Adapter) LocalAddr() net.Addr { return a.local }

// Finish half-closes the stream's write side by sending a FIN. yamux's Close is
// already a half-close, so Finish is a thin alias kept for readability and to
// satisfy the Finisher contract Duplex relies on to preserve the read side.
func (a *Adapter) Finish() error { return a.Close() }

// ForceClose unblocks any read or write stalled on the stream and closes it
// outright, unlike Finish which only releases the write side. It is used when a
// fatal error or a cancelled session demands immediate teardown.
func (a *Adapter) ForceClose() error {
	// A past deadline makes a blocked Read/Write return at once.
	_ = a.SetDeadline(time.Now().Add(-time.Second))
	return a.Close()
}

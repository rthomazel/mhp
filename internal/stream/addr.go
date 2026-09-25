// Package stream provides the primitives that bridge a yamux stream to the
// net.Conn world: a net.Addr the mux hands back no address with, and a
// cancellation-aware duplex copy that moves bytes between a pair of
// net.Conns.
//
// Duplex is the heart of the relay/exit stream pairing. It honours the
// FIN/half-close contract required by SOCKS5 forwarding: an ordinary EOF on
// one direction closes only that direction's write side, preserving any
// response bytes already flowing the other way. Only a fatal error or a
// cancelled context escalates to a forced close of both directions, and the
// caller joins both copy goroutines before returning.
package stream

import "net"

// StreamAddr is a net.Addr describing one end of a bridged stream. yamux
// streams expose no RemoteAddr/LocalAddr, so the relay and exit fill these in
// from what they know about the session that owns the stream.
type StreamAddr struct {
	network string
	address string
}

// NewStreamAddr builds a StreamAddr from a network and address.
func NewStreamAddr(network, address string) *StreamAddr {
	return &StreamAddr{network: network, address: address}
}

// Network implements net.Addr.
func (a *StreamAddr) Network() string {
	if a.network == "" {
		return "tcp"
	}
	return a.network
}

// String implements net.Addr, falling back to the address portion when the
// network is unset so callers can print a sensible descriptor.
func (a *StreamAddr) String() string {
	if a.network == "" {
		return a.address
	}
	return a.network + ":" + a.address
}

// RemoteAddr is the address on the far end of the stream; LocalAddr is the
// address of the endpoint that created the stream. net.Conn requires both,
// so StreamAddr can describe either depending on how the adapter wires it.
func (a *StreamAddr) RemoteAddr() net.Addr { return a }
func (a *StreamAddr) LocalAddr() net.Addr  { return a }

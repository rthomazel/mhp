package exit

import "net"

// policy enforces the approved Internet-only destination policy: the exit
// dials only publicly routable TCP endpoints and refuses loopback, private,
// link-local, multicast and unspecified destinations, including IPv4-mapped
// IPv6 forms. This closes the SSRF door a malicious SOCKS client could otherwise
// poke through the relay.
//
// The zero value is unusable; use newPolicy. The loopback exception is
// test-only and toggled exclusively through allowLoopbackForTest, which keeps
// production refusals honest.
type policy struct {
	allowLoopback bool
}

func newPolicy() *policy { return &policy{} }

// allowLoopbackForTest opts the loopback range into the permitted set. It exists
// purely so the integration test can exercise the "denied reply" path without
// weakening the shipped policy; production never calls it.
func (p *policy) allowLoopbackForTest(v bool) { p.allowLoopback = v }

// allowed reports whether dialing ip is permitted. ip is the actual resolved
// destination address, validated once before dial — never re-resolved.
func (p *policy) allowed(ip net.IP) bool {
	// Normalise IPv4-mapped IPv6 (::ffff:a.b.c.d) to its 4-byte form so the
	// checks below see the real address rather than a disguised loopback or
	// private range.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}

	switch {
	case ip.IsLoopback():
		return p.allowLoopback
	case ip.IsPrivate():
		return false
	case ip.IsLinkLocalUnicast():
		return false
	case ip.IsLinkLocalMulticast():
		return false
	case ip.IsInterfaceLocalMulticast():
		return false
	case ip.IsMulticast():
		return false
	case ip.IsUnspecified():
		return false
	// Limited broadcast (255.255.255.255) is not a routable destination.
	case ip.To4() != nil && isBroadcastV4(ip):
		return false
	}
	return true
}

// isBroadcastV4 reports whether the IPv4 form is the limited broadcast address.
func isBroadcastV4(ip net.IP) bool {
	b := ip.To4()
	return b[0] == 255 && b[1] == 255 && b[2] == 255 && b[3] == 255
}

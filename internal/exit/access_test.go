package exit

import (
	"net"
	"testing"
)

// Tests for the Internet-only destination policy enforced at the exit. Each
// entry is a destination the exit must refuse; the mapped cases prove the
// policy inspects the real address rather than trusting a disguise.

// forbidden lists addresses that must never be dialed, regardless of how they
// arrive, including IPv4-mapped IPv6 encodings that hide a private/loopback
// range.
var forbidden = []string{
	"127.0.0.1",        // loopback
	"::1",              // loopback (native IPv6)
	"::ffff:127.0.0.1", // IPv4-mapped loopback
	"10.0.0.5",         // RFC1918 private
	"192.168.1.9",      // RFC1918 private
	"172.16.32.1",      // RFC1918 private
	"169.254.1.1",      // link-local unicast
	"fe80::1",          // link-local unicast (native IPv6)
	"ff02::1",          // link-local multicast
	"0.0.0.0",          // unspecified
	"::",               // unspecified (native IPv6)
	"224.0.0.1",        // multicast
	"255.255.255.255",  // broadcast/unspecified
}

// allowed lists genuinely routable public addresses that must be permitted.
var allowed = []string{
	"1.1.1.1",
	"8.8.8.8",
	"93.184.216.34",
	"2606:2800:220:1:248:1893:25c8:1946", // a real public IPv6
}

func mustIP(s string) net.IP {
	return net.ParseIP(s)
}

func TestPolicyRejectsForbidden(t *testing.T) {
	p := newPolicy()
	for _, tc := range forbidden {
		if p.allowed(mustIP(tc)) {
			t.Errorf("policy permitted %s; expected refusal", tc)
		}
	}
}

func TestPolicyAllowsPublic(t *testing.T) {
	p := newPolicy()
	for _, tc := range allowed {
		if !p.allowed(mustIP(tc)) {
			t.Errorf("policy refused %s; expected permission", tc)
		}
	}
}

func TestPolicyLoopbackExceptionIsTestOnly(t *testing.T) {
	p := newPolicy()
	// Shipped policy refuses loopback.
	if p.allowed(mustIP("127.0.0.1")) {
		t.Fatal("loopback must be refused in the shipped policy")
	}
	// The exception only flips once the test opts in.
	p.allowLoopbackForTest(true)
	if !p.allowed(mustIP("127.0.0.1")) {
		t.Fatal("loopback must be permitted once opted in")
	}
}

func TestPolicyMappedIPv4Normalization(t *testing.T) {
	p := newPolicy()
	// A private address smuggled as an IPv4-mapped IPv6 literal must still be
	// refused, proving the normalisation catches disguised ranges.
	if p.allowed(mustIP("::ffff:10.1.2.3")) {
		t.Fatal("mapped private address must be refused")
	}
	// And a public address likewise mapped must be permitted.
	if !p.allowed(mustIP("::ffff:8.8.4.4")) {
		t.Fatal("mapped public address must be permitted")
	}
}

package relay

import (
	"io"
	"net"
	"testing"

	"github.com/hashicorp/yamux"

	"github.com/rthomazel/mhp/internal/config"
)

// muxPair builds a connected client/server yamux session pair over net.Pipe so
// the registry can be exercised with real sessions.
func muxPair(t *testing.T) (*yamux.Session, *yamux.Session) {
	t.Helper()
	cc, sc := net.Pipe()
	client, err := yamux.Client(cc, testConfig())
	if err != nil {
		t.Fatalf("yamux client: %v", err)
	}
	server, err := yamux.Server(sc, testConfig())
	if err != nil {
		t.Fatalf("yamux server: %v", err)
	}
	return client, server
}

func testConfig() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.LogOutput = io.Discard
	return cfg
}

// TestRegisterReturnsPrevious ensures the previous holder is returned to the
// caller so it can be closed outside the registry lock.
func TestRegisterReturnsPrevious(t *testing.T) {
	r := NewRegistry()

	first, _ := muxPair(t)
	prev, gen := r.Register(config.ModeExit, first)
	if prev != nil {
		t.Fatalf("first register: want nil previous, got %v", prev)
	}
	if gen == 0 {
		t.Fatalf("first register: want nonzero generation")
	}

	second, _ := muxPair(t)
	prev, _ = r.Register(config.ModeExit, second)
	if prev != first {
		t.Fatalf("second register: want previous == first, got %v", prev)
	}

	current, _, ok := r.Current(config.ModeExit)
	if !ok || current != second {
		t.Fatalf("register did not replace the holder")
	}
}

// TestStaleDetectsSupersession ensures a serving loop that discovers a newer
// generation sees itself as stale and stops.
func TestStaleDetectsSupersession(t *testing.T) {
	r := NewRegistry()

	old, _ := muxPair(t)
	_, gen := r.Register(config.ModeExit, old)

	if r.Stale(config.ModeExit, gen) {
		t.Fatalf("just-registered generation must not be stale")
	}

	newSess, _ := muxPair(t)
	r.Register(config.ModeExit, newSess)

	if !r.Stale(config.ModeExit, gen) {
		t.Fatalf("superseded generation must be reported stale")
	}
}

// TestVacateLeavesNewerRegistration ensures Vacate only clears a slot the
// declaring generation still owns. A stale generator must not evict the
// holder that replaced it.
func TestVacateLeavesNewerRegistration(t *testing.T) {
	r := NewRegistry()

	old, _ := muxPair(t)
	_, gen := r.Register(config.ModeExit, old)

	newSess, _ := muxPair(t)
	_, newGen := r.Register(config.ModeExit, newSess)

	// A stale vacate is a no-op: the new holder survives.
	r.Vacate(config.ModeExit, gen)
	if _, _, ok := r.Current(config.ModeExit); !ok {
		t.Fatalf("stale vacate must not evict the new holder")
	}

	// A fresh vacate releases the slot cleanly.
	r.Vacate(config.ModeExit, newGen)
	if _, _, ok := r.Current(config.ModeExit); ok {
		t.Fatalf("fresh vacate should release the slot")
	}
}

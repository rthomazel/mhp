package relay

import (
	"sync"

	"github.com/hashicorp/yamux"
	"github.com/rthomazel/mhp/internal/config"
)

// slot holds the session currently occupying a role together with the
// generation number that identifies it.
type slot struct {
	mux *yamux.Session
	gen uint64
}

// Registry tracks at most one live session per role. A new same-role
// registration replaces the old holder; the old holder is returned to the
// caller so it can be closed outside the lock. The registry never closes a
// session itself, which keeps lock-held time short and lets the caller
// perform network teardown without risking a deadlock.
type Registry struct {
	mu    sync.Mutex
	slots map[config.Mode]*slot
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{slots: map[config.Mode]*slot{}}
}

// Register places mux in the slot for mode, replacing any previous holder. It
// returns the previous session (nil when the slot was empty) and the
// generation number assigned to the new holder. The previous session must be
// closed by the caller OUTSIDE the lock.
func (r *Registry) Register(mode config.Mode, mux *yamux.Session) (prev *yamux.Session, gen uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	e := r.slots[mode]
	if e == nil {
		e = &slot{}
		r.slots[mode] = e
	}
	prev = e.mux
	e.gen++
	e.mux = mux
	return prev, e.gen
}

// Current reports the session and generation currently occupying mode. The ok
// result is false when the slot is empty.
func (r *Registry) Current(mode config.Mode) (mux *yamux.Session, gen uint64, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	e := r.slots[mode]
	if e == nil || e.mux == nil {
		return nil, 0, false
	}
	return e.mux, e.gen, true
}

// Stale reports whether gen no longer matches the session occupying mode. It
// is true when the slot is empty or held by a different generation, which is
// the signal a serving loop uses to know it has been superseded.
func (r *Registry) Stale(mode config.Mode, gen uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	e := r.slots[mode]
	if e == nil || e.mux == nil {
		return true
	}
	return e.gen != gen
}

// Vacate drops the slot when gen still owns it, so a serving loop that has
// exited can cleanly release a role that has not yet been re-registered. If a
// newer registration has taken over, Vacate leaves it untouched: the old
// generation must not erase the new one.
func (r *Registry) Vacate(mode config.Mode, gen uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	e := r.slots[mode]
	if e == nil || e.gen != gen {
		return
	}
	e.mux = nil
	e.gen++
}

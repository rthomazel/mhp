package transport

import (
	"errors"
	"fmt"
	"time"
)

// ErrInvalidTiming is returned when Timing.validate rejects an impossible schedule.
var ErrInvalidTiming = errors.New("timing: invalid configuration")

// Timing bundles every duration the transport relies on, from the plan's bounds.
//
// Durations split into two budgets: dial, handshake, and setup bound the
// pre-mux exchange, while keepalive and ping bound the mux heartbeat once a
// session is healthy.
type Timing struct {
	// DialTimeout bounds the TCP handshake.
	DialTimeout time.Duration
	// HandshakeTimeout bounds the TLS handshake.
	HandshakeTimeout time.Duration
	// SetupTimeout bounds the hello/response exchange over the open TLS
	// session. It is cleared before the mux owns the socket so it never
	// poisons stream reads and writes.
	SetupTimeout time.Duration
	// KeepAliveInterval is the yamux probe cadence on an idle session.
	KeepAliveInterval time.Duration
	// PingTimeout is yamux tolerance for an unanswered probe before the
	// session is judged dead.
	PingTimeout time.Duration
	// StreamOpenTimeout is the deadline offered to a freshly opened stream.
	StreamOpenTimeout time.Duration
	// DrainTimeout grants a half-closed stream before its partner is
	// force-closed.
	DrainTimeout time.Duration
	// MinBackoff is the reconnect backoff floor.
	MinBackoff time.Duration
	// MaxBackoff is the reconnect backoff ceiling.
	MaxBackoff time.Duration
	// HealthyWindow is the session uptime that resets the backoff to the floor.
	HealthyWindow time.Duration
	// JitterRatio is the randomized fraction added to each backoff delay.
	JitterRatio float64
}

// DefaultTiming is the production schedule taken verbatim from the plan bounds.
var DefaultTiming = Timing{
	DialTimeout:       10 * time.Second,
	HandshakeTimeout:  10 * time.Second,
	SetupTimeout:      10 * time.Second,
	KeepAliveInterval: 15 * time.Second,
	PingTimeout:       10 * time.Second,
	StreamOpenTimeout: 10 * time.Second,
	DrainTimeout:      30 * time.Second,
	MinBackoff:        1 * time.Second,
	MaxBackoff:        30 * time.Second,
	HealthyWindow:     60 * time.Second,
	JitterRatio:       0.2,
}

// validate rejects an impossible schedule, e.g. a zero dial timeout or a backoff
// whose floor sits above its ceiling.
func (t Timing) validate() error {
	durations := map[string]time.Duration{
		"DialTimeout":       t.DialTimeout,
		"HandshakeTimeout":  t.HandshakeTimeout,
		"SetupTimeout":      t.SetupTimeout,
		"KeepAliveInterval": t.KeepAliveInterval,
		"PingTimeout":       t.PingTimeout,
		"StreamOpenTimeout": t.StreamOpenTimeout,
		"DrainTimeout":      t.DrainTimeout,
		"MinBackoff":        t.MinBackoff,
		"MaxBackoff":        t.MaxBackoff,
		"HealthyWindow":     t.HealthyWindow,
	}
	for name, d := range durations {
		if d <= 0 {
			return fmt.Errorf("%w: %s must be positive", ErrInvalidTiming, name)
		}
	}
	if t.MinBackoff > t.MaxBackoff {
		return fmt.Errorf("%w: MinBackoff %s exceeds MaxBackoff %s", ErrInvalidTiming, t.MinBackoff, t.MaxBackoff)
	}
	if t.HealthyWindow < t.MinBackoff {
		return fmt.Errorf("%w: HealthyWindow %s is below MinBackoff %s", ErrInvalidTiming, t.HealthyWindow, t.MinBackoff)
	}
	if t.JitterRatio < 0 || t.JitterRatio > 1 {
		return fmt.Errorf("%w: JitterRatio %v must be within 0..1", ErrInvalidTiming, t.JitterRatio)
	}
	return nil
}

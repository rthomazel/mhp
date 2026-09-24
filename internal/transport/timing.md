# Vars

## ErrInvalidTiming = "timing: invalid configuration"

Returned when Timing.validate rejects an impossible schedule, e.g. a zero dial timeout.

# Types

## Timing

1. DialTimeout Duration — maximum time to complete the TCP handshake.
2. HandshakeTimeout Duration — maximum time for the TLS handshake.
3. SetupTimeout Duration — maximum time for the hello/response exchange once the TLS session is up.
4. KeepAliveInterval Duration — yamux probe cadence on an idle session.
5. PingTimeout Duration — yamux tolerance for an unanswered probe before the session is judged dead.
6. StreamOpenTimeout Duration — deadline offered to a freshly opened stream.
7. DrainTimeout Duration — grace granted to a half-closed stream before its partner is force-closed.
8. MinBackoff Duration — reconnect backoff floor.
9. MaxBackoff Duration — reconnect backoff ceiling.
10. HealthyWindow Duration — session uptime that resets the backoff to the floor.
11. JitterRatio float64 — randomized fraction added to each backoff delay.

## DefaultTiming = Timing{DialTimeout: 10s, HandshakeTimeout: 10s, SetupTimeout: 10s, KeepAliveInterval: 15s, PingTimeout: 10s, StreamOpenTimeout: 10s, DrainTimeout: 30s, MinBackoff: 1s, MaxBackoff: 30s, HealthyWindow: 60s, JitterRatio: 0.2}

Production values taken from the plan bounds table. Tests overwrite the durations to shrink them; JitterRatio stays inside 0..1.

# Functions

## Timing.validate() error

1. Require DialTimeout, HandshakeTimeout, SetupTimeout, KeepAliveInterval, PingTimeout, StreamOpenTimeout, DrainTimeout, MinBackoff, MaxBackoff, and HealthyWindow to be strictly positive.
2. Require MinBackoff no greater than MaxBackoff.
3. Require HealthyWindow no less than MinBackoff.
4. Require JitterRatio inside 0..1.
5. return nil.

#### Errors

- **1.** if any duration is not strictly positive, return ErrInvalidTiming naming the field.

---

- **2.** if MinBackoff exceeds MaxBackoff, return ErrInvalidTiming.

---

- **3.** if HealthyWindow is below MinBackoff, return ErrInvalidTiming.

---

- **4.** if JitterRatio is outside 0..1, return ErrInvalidTiming.

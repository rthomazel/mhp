// Package relay implements the middle leg of the MHP topology: it owns the
// role registry that tracks one live exit slot and one live proxy slot, and
// the stream bridge that pairs an accepted proxy stream with a freshly opened
// exit stream and copies bytes between them.
//
// The registry follows the generation-isolation pattern: each registration
// bumps a per-role generation counter, and the previous holder is handed back
// to the caller so it can be closed OUTSIDE the registry lock. Cleanup checks
// the current generation before tearing anything down, so retiring an old
// session can never evict the registration that replaced it.
//
// The bridge pairs proxy-originated streams toward the current exit session.
// It never pairs exit-originated streams back toward the proxy (that would be
// an unexpected reverse stream), and when no exit is available it closes the
// incoming stream promptly rather than queuing it.
package relay

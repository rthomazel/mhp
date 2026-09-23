// Package transport implements the relay-facing TLS handshake that authenticates
// an MHP client before opening a yamux session.
//
// Auth is bearer-token based: the client presents a role-specific token after
// the TLS handshake, the relay answers with a verdict, and only on success does
// the relay start yamux. Framing is JSON-over-length-prefixed frames (see
// framing.md); record shapes live in records.md.
//
// The client half (authenticator.go) drives the full dial→handshake→hello→mux
// journey. The relay half (handshake.go) accepts a TLS connection, validates the
// presented bearer, and starts yamux only on success.
package transport

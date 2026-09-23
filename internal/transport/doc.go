Package transport authenticates MHP clients to the relay over a private-CA TLS connection and opens a yamux session once authentication succeeds. It speaks a small JSON control record before handing framing to yamux.

The package holds both halves of that handshake: Authenticator drives the client side (dial, handshake, hello/response, mux start), and the relay side (handshake.go, handshake.md) accepts a TLS connection, validates the presented bearer, replies with the verdict, and starts yamux only on success.

The package owns only the TLS socket and the mux session. It trusts the relay CA (never InsecureSkipVerify), authenticates with a bearer token, and refuses to create a session before the relay accepts. Registration, stream pairing, and destination dialing live in the relay, proxy, and exit packages.

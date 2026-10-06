---
id: 2026-10-06-shared-container-image
type: spec
summary: One reusable MHP container image with runtime mode selection.
author: Thom
agents: Merlin
created: 2026-10-06
---

# Shared container image

Operators can build one image and reuse it for relay, exit-node, or proxy,
choosing the mode and its options when starting the container with Compose.
No mode-specific rebuild or embedded credentials are required. Existing relay
deployment remains available using the shared image.

The canonical mode name is exit-node. Examples, help, and protocol descriptions
must agree with that spelling; exit and exit-note are not mode aliases.
Internal identifiers and existing credential filenames need not be renamed.

Container packaging must preserve verified TLS, external read-only credentials,
clean signal handling, and the proxy's loopback-only listener restriction.
Containerizing the proxy must not expose unauthenticated SOCKS to the network.
Windows native launchers remain supported. Image publication and production
deployment are outside this change.

## Acceptance

- A root-level Dockerfile produces a single image capable of all three modes.
- Compose can select the mode through command arguments or an explicit binary
  entrypoint; no shell wrapper consumes or rewrites arguments.
- The existing relay Compose configuration uses the shared Dockerfile and
  retains its runtime configuration.
- Documentation includes each mode, required credential mounts, and the proxy
  network-namespace constraint.
- Mode validation accepts exit-node and rejects exit and exit-note. Protocol
  documentation reflects the already-implemented exit-node wire value.

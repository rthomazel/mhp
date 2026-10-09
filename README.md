# mhp

My HTTP Proxy: browser traffic through a relay to a Windows exit client.

Architecture, libraries, operational behavior, and wire protocol live in
[the implementation plan](spc/plan.md). Tasks 1-5 are done: one binary
dispatches three modes (`relay`, `exit-node`, `proxy`) with verified private-CA
TLS, a yamux session per authenticated client, SOCKS5 at the exit, and a
SOCKS5 proxy for the browser, packaged with one reusable container
image and Windows launchers.

Packaging and run instructions — relay container, Windows exit launcher,
Firefox configuration, and the verification procedure — are in
[docs/DEPLOYMENT.md](docs/DEPLOYMENT.md). The relay container and its
credentials live in [deploy/](deploy/). Build the shared image from the root
with `docker build -t mhp:latest .`; select `-mode relay`, `-mode exit-node`,
or `-mode proxy` through Compose `command` or an explicit binary `entrypoint`.
See the runbook for credential mounts and the proxy's host-network requirement.

Implementation checklist and verification: [spc/tasks.md](spc/tasks.md).

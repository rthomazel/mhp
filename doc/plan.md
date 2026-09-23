---
id: 2026-09-23-mhp-plan
type: documentation
summary: Review draft for the MHP browser proxy architecture and implementation.
author: Thom
agents: Merlin
created: 2026-09-23
---

# MHP — My HTTP Proxy

Status: **design draft for review; not implemented**. This document is the implementation reference requested by Thom. Proposed defaults below are reviewable decisions, not claims of operator approval.

## Goal and agreed scope

Basic web browsing from Firefox through an always-on Windows machine. One Go binary, three `-mode` values:

- **relay**: fixed public IP in Germany; coordinates sessions and relays traffic.
- **exit**: Windows exit client, connects outbound to relay and opens website TCP connections.
- **proxy**: proxy client, exposes SOCKS5 on localhost for the browser.

```
Firefox -- SOCKS5 --> proxy client -- TLS --> relay <-- TLS -- exit client --> website
```

Both clients initiate outbound TLS/TCP connections. No inbound Windows listener, peer-to-peer discovery, WireGuard, TUN driver, NAT configuration, or system routing changes. Relay may see proxied data; no extra end-to-end encryption layer is required. Website HTTPS remains browser-to-website encrypted: never intercept certificates.

One exit and one proxy installation initially, multiple concurrent browser connections. No UDP/QUIC, SOCKS BIND, performance benchmark suite, download optimization, DNS policy subsystem, active-flow migration, or Windows service packaging. Browser TCP fallback must be verified in the browsing acceptance test. DNS may resolve locally or at Windows.

Exit runs manually from a BAT file, stays alive, and retries network connections forever. Existing website connections may fail during outages; refresh after recovery is acceptable. Automatic Windows startup/service installation is a follow-up.

## Architecture decision: forward SOCKS5 intact

**Proposed:** each accepted localhost TCP connection becomes one logical stream across each relay leg. Relay copies the SOCKS5 exchange and subsequent bytes unchanged. The SOCKS5 server runs at the exit, not the proxy client.

This avoids a custom destination-request protocol. Firefox sends an IP or hostname; the exit SOCKS5 handler resolves hostnames and dials the destination. Success is sent only after destination dial succeeds. Relay never dials websites or resolves destination names.

Each client maintains one authenticated TLS connection, multiplexed with `github.com/hashicorp/yamux`. Both outbound clients use `yamux.Client`; each accepted relay connection uses `yamux.Server`. Either endpoint can open streams:

1. Proxy accepts local browser TCP connection and snapshots the current relay session.
2. Proxy opens a yamux stream to relay.
3. Relay accepts it, snapshots the current exit session, and opens a stream toward exit.
4. Relay copies both ways between streams.
5. Exit accepts stream and passes a connection adapter to SOCKS5 `ServeConn`.
6. Exit SOCKS5 handler dials website TCP and forwards bytes.

No exit available: relay closes the incoming stream promptly; the browser may see a closed SOCKS connection rather than a structured SOCKS error. Do not invent a partial SOCKS parser solely to improve this message. Log `exit_unavailable`. Do not queue requests for a future session or replay application bytes after reconnect.

### Session ownership

Use an in-memory registry, no database. One active exit slot and one active proxy slot. Proposed policy: a newly authenticated connection replaces the old same-role session. Replacement closes the old session and its streams. Give each registration a generation ID; cleanup may clear a slot only if it still owns that generation. Never hold registry locks during network I/O.

Relay accepts streams only from proxy sessions and opens streams only toward exits. Reject unexpected reverse streams; the exit must not be able to request website connections through the proxy. Closing a proxy connection closes its associated relay/exit streams, not the entire exit session.

## Libraries investigated

Source inspected locally; these are candidates, not installed dependencies. Pin a reviewed release or exact revision in `go.mod` during implementation, run vulnerability checks, and retain applicable license notices.

| Candidate | Findings and recommendation |
| --- | --- |
| Go stdlib | Use `crypto/tls`, `crypto/x509`, `net`, `io`, `context`, `os/signal`, `log/slog`, `flag`, `encoding/json`, and synchronization primitives. No web framework needed. |
| [hashicorp/yamux](https://github.com/hashicorp/yamux/tree/ccd743982369f0799070e0a8fa24211830a24685) | Recommended for stream multiplexing, flow control and keepalive. Inspected `mux.go`, `session.go`, `stream.go`. `Client`, `Server`, `OpenStream`, `AcceptStreamWithContext`, `CloseChan` provide the required lifecycle. No custom data framing needed. |
| [things-go/go-socks5](https://github.com/things-go/go-socks5/tree/9fd5240192b70c31c67f60bb5815f977bdb2d9db) | Recommended SOCKS5 implementation. `ServeConn`, `WithResolver`, `WithDial`, and `WithRule` allow integration; recheck APIs at the pinned version. Explicitly permit CONNECT only: upstream also implements UDP association. |
| [armon/go-socks5](https://github.com/armon/go-socks5/tree/e75332964ef517daa070d7c38a9466a0d687e0a5) | Older alternative; inspected head dated 2016, no go.mod. Prefer the maintained things-go variant. |
| Separate TLS connection per browser flow | Avoids a mux dependency but requires matching reverse connections, additional authentication and a control channel to instruct Windows to connect back. More custom lifecycle work; not selected. |

### Integration details that must not be missed

- SOCKS library creates `context.Background()` internally. Inject resolver/dial callbacks deriving deadlines from the exit session context; don't assume its callback context is cancellable on shutdown.
- Use a bounded `net.Resolver.LookupIPAddr` implementation and `net.Dialer.DialContext`. SOCKS library resolves hostname before dialing; default resolver is not the desired lifecycle boundary.
- SOCKS library buffers the request. Preserve its buffered reader when forwarding; do not read payload from the original stream and lose read-ahead bytes.
- Upstream SOCKS forwarding calls `CloseWrite` if available. Yamux stream exposes `Close`, not `CloseWrite`, and its close is FIN-style. Provide a tested adapter exposing `CloseWrite` as stream FIN, keeping the read direction usable for remaining responses.
- Bidirectional copy: two bounded-buffer `io.Copy` loops; EOF half-closes the destination write direction. Do not immediately kill the reverse direction on ordinary EOF. Fatal error/session cancellation sets deadlines and closes endpoints to unblock both goroutines; join them. A second yamux `Close` alone is not a guaranteed force-cancel of a half-closed stream.
- Don't put a short lifetime deadline on browsing data after setup. Clear setup deadlines after SOCKS CONNECT succeeds; use a small custom connect handler if necessary to establish this boundary. The library's hook runs after resolution: cancellation must also cover resolution.
- Adapters must expose valid `net.Conn` addresses/deadlines. SOCKS success should report destination socket's bound address, not TLS relay address.
- TCP multiplexing still has transport head-of-line blocking. Accepted for basic browsing, not advertised as loss-independent streams.

## TLS, authentication, and initial wire exchange

**Proposed for review:** private CA and relay certificate, supplied as files. Dial the fixed relay IP while setting a separate TLS server name. A private-CA certificate may use the tested marker `en.zalando.de`; this is a routing/camouflage marker, not a claim of ownership, domain fronting, or use of Zalando infrastructure. Alternatively use an operator-owned hostname. Trust the private CA only in MHP, never install it as a browser/system-wide root. Verify normal certificate chain, name, and validity. Never copy netdiag's `InsecureSkipVerify` into MHP. No insecure fallback.

Relay uses its own TCP/443 listener. **Deployment blocker:** confirm available IP/port before deployment; netdiag or other services may already own 443. Don't silently stop them. Integration with another listener is outside initial implementation.

Separate high-entropy exit and proxy bearer tokens, carried only after verified TLS. Load secrets from role-specific files or environment, not CLI arguments; constant-time comparisons, no token logging. Local SOCKS has no authentication because it binds loopback only; other local users can use it, an accepted single-user-machine assumption requiring review.

Before starting yamux, exchange bounded control records over TLS:

- uint32 big-endian length, followed by JSON (maximum 4096 bytes).
- Client hello: `version: 1`, `role: exit|proxy`, `token`.
- Relay response: `version: 1`, `status: ok|error`, non-secret `session_id` on success, bounded error code otherwise.
- Reject invalid version/role/length/authentication and close. Never create/register yamux before authentication succeeds.
- Entire TLS handshake plus hello/response bounded by setup deadline; clear deadline before starting mux.
- Use `io.ReadFull` and complete-write handling. No buffered read-ahead across the switch to yamux unless that reader is explicitly carried forward.

No custom application records after this exchange: yamux owns framing and each stream contains SOCKS5 bytes followed by TCP payload.

## Reconnection and bounds

Both clients run `connect → verify TLS → authenticate → serve session → cleanup → backoff → retry` until process cancellation. Listen on localhost independently of proxy relay availability; close new browser connections promptly when disconnected.

Proposed defaults (constants initially; tune only from evidence):

| Bound | Default |
| --- | --- |
| TCP dial / TLS and authentication setup | 10s each, cancellation-aware |
| Retry delay | exponential 1s to 30s, jitter, capped at 30s |
| Backoff reset | after 60s healthy session, not immediately on connect |
| Yamux keepalive interval | 15s |
| Yamux connection write / ping timeout | 10s |
| Stream open timeout | 10s (upstream default is 75s) |
| Browser SOCKS setup including resolve/dial | 15s total |
| Destination dial | at most 10s within remaining setup budget |
| Half-closed stream drain | 30s before forced cleanup |
| Active browser streams | 64 globally at relay/exit and locally at proxy |
| Concurrent unauthenticated relay connections | 16; excess closed |

Use yamux's existing heartbeat rather than implementing another protocol. With inspected implementation, failed idle path detection is approximately interval plus ping timeout, not a hard real-time guarantee. Inspect behavior at the pinned revision. Connection errors may detect failure sooner.

Network failures retry forever; malformed local configuration exits nonzero. Authentication/certificate failures never fall back to insecure transport: report prominently and retry at capped delay so operator provisioning fixes can recover without restart. On shutdown cancel retry timers, close listeners and sessions, close active destination sockets, and wait for owned goroutines. No unbounded queues or per-request retry loops.

## CLI, debug output, and Windows launch

Illustrative interface (credential flags name files, never literal secrets):

```sh
mhp -mode relay -listen :443 -tls-cert relay.crt -tls-key relay.key -exit-token-file exit.token -proxy-token-file proxy.token
mhp -mode exit -relay <ip>:443 -tls-name en.zalando.de -ca relay-ca.crt -token-file exit.token -debug
mhp -mode proxy -relay <ip>:443 -tls-name en.zalando.de -ca relay-ca.crt -token-file proxy.token -listen 127.0.0.1:1080 -debug
```

Validate mode-specific options. Proxy listen address must be loopback; no accidental `0.0.0.0`. Firefox: SOCKS v5, localhost port 1080. Proxy-DNS checkbox may be on or off; neither is a correctness requirement.

All modes support `-debug`: console output plus `mhp-debug-{mode}-{unix-seconds}.txt` in current working directory. Use exclusive create with a suffix on collision rather than truncating an existing same-second log. File creation failure is an explicit startup error. Files may reveal destinations; restrictive permissions where supported, and no payloads/tokens/private keys. Default logs report startup, connection state and errors; debug adds stream IDs, durations, bytes, reconnect reasons. Avoid full URLs or HTTP parsing.

Prefer an injected `slog.Logger` writing to console or `io.MultiWriter(console, file)` over global stdout redirection; adapt dependency logging into it. Flush/close before returning from main. No debug rotation in v1: debug is opt-in diagnostic capture, not recommended indefinitely on an unattended process.

```bat
@echo off
cd /d "%~dp0"
mhp.exe -mode exit -relay <ip>:443 -tls-name en.zalando.de -ca relay-ca.crt -token-file exit.token -debug
pause
```

Console must stay open. BAT does not implement retries; the Go process does. Ctrl+C stops cleanly. Build Windows amd64 with `CGO_ENABLED=0`; no installer/admin rights expected. Service packaging/start-on-boot deferred.

## Package boundaries and implementation order

Keep concrete types and narrow interfaces; no generic plugin framework.

```
cmd/mhp/             flags, mode dispatch, signals, process exit
internal/config/     mode-specific validation and credential loading
internal/logging/    console/file setup and dependency adapters
internal/transport/  TLS, auth records, yamux config, session reconnect loop
internal/relay/      role registry, replacement generations, stream pairing
internal/proxy/      loopback listener, bridge to relay streams
internal/exit/       SOCKS handler, destination resolve/dial and access rules
internal/stream/     net.Conn adapter and cancellation-aware duplex copy
```

Transport owns TLS socket and mux session. Each stream handler owns its stream pair/destination connection. Constructors don't launch hidden goroutines; `Run(ctx)` methods own and join work. Main uses `signal.NotifyContext`; return errors rather than calling `os.Exit` from packages.

Implementation sequence:

1. CLI/logging, TLS/auth and reconnecting sessions; validate role rejection and shutdown.
2. Session registry and stream bridge; exercise half-close/cancel behavior before integrating SOCKS.
3. Exit SOCKS handler and proxy localhost listener; local full-topology HTTP/HTTPS test.
4. Cross-build, BAT example, deployment instructions, then three manual acceptance checks.

No production implementation in this documentation PR. Do not consider the design approved merely because this file exists.

## Direct netdiag reuse references

Reference commit: [`2479bb06cccf688a0f43ebce7c4645511901f770`](https://github.com/rthomazel/netdiag/tree/2479bb06cccf688a0f43ebce7c4645511901f770). Local source: `/projects/netdiag`. Read code before copying: some README/test numbering is stale.

- [`cmd/client/main.go`](https://github.com/rthomazel/netdiag/blob/2479bb06cccf688a0f43ebce7c4645511901f770/cmd/client/main.go): `openDebugTrace`, `newStdoutTee`, `stdoutTee.Close`; reuse cwd/timestamp naming and console-plus-file UX, simplify to injected writer and avoid truncation/global stdout mutation.
- [`internal/client/wgtls.go`](https://github.com/rthomazel/netdiag/blob/2479bb06cccf688a0f43ebce7c4645511901f770/internal/client/wgtls.go): `dialTLSOverTLS` shows direct IP dial with marker SNI and `HandshakeContext`. Replace disabled verification and apply post-handshake auth deadlines.
- [`internal/protocol/protocol.go`](https://github.com/rthomazel/netdiag/blob/2479bb06cccf688a0f43ebce7c4645511901f770/internal/protocol/protocol.go): marker `en.zalando.de`, framing helpers; reuse exact-read/bounded-frame discipline, not WG packet types/checksum format.
- [`internal/wgtest/wtls.go`](https://github.com/rthomazel/netdiag/blob/2479bb06cccf688a0f43ebce7c4645511901f770/internal/wgtest/wtls.go): lifecycle/generation isolation and concurrent TLS read/write lessons. Do not copy `conn.Bind`, WG queues, or rekey machinery: yamux replaces packet framing.
- [`internal/server/server.go`](https://github.com/rthomazel/netdiag/blob/2479bb06cccf688a0f43ebce7c4645511901f770/internal/server/server.go): listener ownership and shutdown patterns; MHP does not need its HTTPS control plane or SNI routing mux initially.
- [`doc/deploy.md`](https://github.com/rthomazel/netdiag/blob/2479bb06cccf688a0f43ebce7c4645511901f770/doc/deploy.md): pure-Go Windows cross-build and manual launch approach.

Copy/adapt small relevant helpers rather than importing another repo's `internal` packages. Check reuse permission/notices before copying: no tracked LICENSE was found in the inspected netdiag clone. Both repos are operator-owned; record authorization or add licensing clarification, don't assume a license.

Evidence: September 23 run passed real WG handshake inside TLS and failed native WG on three ports. This supports testing TLS transport; does not prove sustained proxy usability, DPI mechanism, or necessity of the marker SNI. No camouflage guarantee or JA3/JA4 evasion claim.

## Minimum verification

Only three manual acceptance scenarios, plus focused automated checks needed to implement them safely:

1. **Browse:** Firefox configured to localhost SOCKS5 loads HTTP and HTTPS pages with parallel resources. Controlled destination confirms Windows-side public egress, not relay IP. Test hostname and IP targets. No throughput benchmark.
2. **Idle:** leave connected and unused for 10 minutes, then browse; no manual restart. Log whether original session survived or reconnect occurred.
3. **Recover:** disconnect Windows Internet for 30 seconds and restore; repeat with relay restart. Requests during outage fail boundedly; new page loads recover automatically (target within 60s of network/server availability). Existing pages may need refresh.

Automated: bounded auth records/version/role/wrong-token rejection, TLS trust/name rejection, CONNECT-only policy, EOF half-close and cancellation, generation-safe replacement, and one loopback full-topology HTTP/HTTPS integration test with forced exit reconnect. Cross-build Windows; `go test -race ./...`, `go vet ./...`, formatting and dependency vulnerability check. Tests use temporary CA/tokens, ephemeral ports and short injectable retry intervals. These are correctness checks, not an expanded resilience project.

## Review decisions and open questions

Already agreed: names/topology, single binary with three modes, SOCKS5 browser interface, stdlib-first/reuse netdiag, debug files in cwd, manual BAT launch, retry forever, minimum tests, no concern about DNS placement or relay access to plaintext HTTP.

Proposals awaiting review:

1. **TLS provisioning/SNI:** private CA + name verification; retain marker SNI initially or use operator-owned name? Who generates/distributes certificate and token files? No public CA issuance for a third-party hostname.
2. **Destination policy:** recommend Internet-only destinations; reject loopback, private, link-local, multicast and unspecified IPs including IPv4-mapped IPv6. Resolve once and validate each concrete IP before dialing it to avoid DNS rebinding. Local tests need an explicit test-only exception. Allow TCP ports generally for browsing redirects; restrict to 80/443 only if desired. This is policy, not yet an approved restriction.
3. **Session replacement:** new authenticated same-role session replaces old; confirm this beats rejecting duplicates for manual launches/reconnects.
4. **Dependencies/stream design:** approve yamux + things-go SOCKS5, intact SOCKS forwarding, and the proposed timeout/limit defaults.
5. **Deployment:** exact relay address and TCP/443 availability alongside netdiag/live services. Confirm endpoint ownership before any listener changes.
6. **Reuse permission:** clarify netdiag licensing before copied code lands.

Pin exact dependency versions and verify adapter/deadline behavior in a small implementation spike before committing to APIs. Any incompatibility should update this plan rather than be papered over with unbounded waits or ad-hoc protocol changes. Windows service packaging, multiple exits, log rotation, and seamless flow recovery remain follow-ups, not blockers for this manual-launch version.

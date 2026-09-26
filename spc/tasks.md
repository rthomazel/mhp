---
id: 2026-09-23-mhp-tasks
type: documentation
summary: Implementation checklist and minimum verification procedures for MHP.
author: Thom
agents: Merlin
created: 2026-09-23
---

# MHP implementation tasks

Status: pending; this PR contains no application code. Requirements and decisions live in [plan.md](plan.md). Complete tasks in order. Private-CA TLS and unauthenticated loopback SOCKS5 are approved. Record validation results with the implementing PR, not fabricated checkmarks here.

## 1. Bootstrap, configuration and logs

- [ ] Create module `github.com/rthomazel/mhp`, pin Go/tool and dependency versions, add reproducible setup/build/test commands. Review dependency licenses and vulnerability results.
- [ ] Implement mode-specific flags, config validation, signal cancellation and returned errors. Reject non-loopback proxy listen addresses. Load token files without printing their contents.
- [ ] Implement injected logging for all modes, console plus cwd debug file with collision-safe naming; close files before process exit. Adapt library loggers, redact credentials and payloads.
- [ ] Verify invalid mode/config, unwritable debug directory, duplicate timestamp filenames and clean Ctrl+C behavior.

References: [CLI/logging requirements](plan.md#cli-debug-output-and-windows-launch), [package boundaries](plan.md#package-boundaries-and-implementation-order), netdiag [debug helpers](https://github.com/rthomazel/netdiag/blob/2479bb06cccf688a0f43ebce7c4645511901f770/cmd/client/main.go).

Done when: one binary dispatches all three modes and logs consistently; no networking implementation implied by stub mode startup.

## 2. Verified TLS, authentication and reconnecting sessions

Depends on task 1. Trust model: self-issued private CA with normal certificate verification enabled.

- [ ] Document reproducible certificate generation (for example, OpenSSL commands): create an offline CA signing key and self-signed `relay-ca.crt`, then a separate `relay.key` and CA-signed `relay.crt` with server-auth usage, explicit validity and a SAN matching `-tls-name`. With the initial marker SNI, use DNS SAN `en.zalando.de`, trusted only inside MHP; direct IP dialing does not require an IP SAN when verifying that DNS name.
- [ ] Provision the relay with `relay.crt` and `relay.key`, wired to `-tls-cert` and `-tls-key`. Mount them read-only from persistent storage and restrict key access to the service account (0600 on Unix). Never bake secrets into images. The relay does not need the CA signing key or client certificates; role tokens authenticate clients.
- [ ] Distribute only public `relay-ca.crt` to both clients through a trusted channel, verify its fingerprint and configure `-ca`. Keep the CA signing key offline with a protected backup; never install this CA in browser/system trust stores.
- [ ] Document separate role-token generation/delivery and renewal before certificate expiry: sign a replacement relay certificate under the same CA, validate key/name/chain/validity, replace files and restart relay, then confirm both clients reconnect. CA replacement requires updating client trust files. Never commit operational private keys/tokens.
- [ ] Validate provisioned TLS from both client modes, including rejection of wrong CA, wrong SAN, expired certificate and mismatched relay key. Record public certificate expiry/fingerprint only, not secrets.
- [ ] Implement bounded TCP dial, TLS handshake, versioned role/token exchange and response, then start yamux only after authentication. Reject oversized/truncated records and clear setup deadlines before mux owns the socket.
- [ ] Configure yamux keepalive, write/open/close timeouts from the spec. Watch session closure; close owned resources on cancellation.
- [ ] Implement cancellable retry-forever loop with jitter, cap and stable-session backoff reset. Reload credential/trust files on retry if recovery from corrected provisioning without process restart is promised.
- [ ] Verify wrong trust/name/token/version/role rejection, no insecure fallback, timeout cleanup, network reconnect and cancellation during backoff. Inject short timing settings for tests rather than sleeping production intervals.

References: [TLS exchange](plan.md#tls-authentication-and-initial-wire-exchange), [bounds](plan.md#reconnection-and-bounds), netdiag [TLS dial](https://github.com/rthomazel/netdiag/blob/2479bb06cccf688a0f43ebce7c4645511901f770/internal/client/wgtls.go), yamux [session API](https://github.com/hashicorp/yamux/blob/ccd743982369f0799070e0a8fa24211830a24685/session.go).

Done when: both roles can authenticate and reconnect to a local relay without leaked sockets or goroutines; negative authentication never registers a session.

## 3. Session registry and stream bridge

Depends on task 2.

- [ ] Implement role slots with generation-safe replacement. Close old sessions outside registry locks; old cleanup must not erase new registration.
- [ ] Accept only proxy-originated streams; pair each with a new exit stream. No exit means prompt close/log, not a queue or application replay. Enforce stream and unauthenticated-connection limits before spawning unbounded work.
- [ ] Implement duplex copy and yamux adapter. Exercise FIN/half-close separately from forced cancellation; preserve remaining response bytes after request EOF. Cancel both copy directions and join their goroutines on fatal errors.
- [ ] Test replacement racing cleanup, missing exit, unexpected reverse streams, EOF/drain timeout and session cancellation while reads/writes block.

References: [session ownership](plan.md#session-ownership), [integration pitfalls](plan.md#integration-details-that-must-not-be-missed), yamux [stream close implementation](https://github.com/hashicorp/yamux/blob/ccd743982369f0799070e0a8fa24211830a24685/stream.go), netdiag [generation isolation](https://github.com/rthomazel/netdiag/blob/2479bb06cccf688a0f43ebce7c4645511901f770/internal/wgtest/wtls.go).

Done when: synthetic bidirectional streams traverse both TLS legs and terminate predictably. Verify actual pinned-library behavior before relying on adapters.

## 4. SOCKS exit and local proxy

Depends on task 3.

- [ ] Proxy accepts localhost TCP and passes each connection intact into its relay stream. Keep listener alive across relay outages; fail new connections promptly while unavailable.
- [ ] Exit passes each incoming stream to SOCKS5 `ServeConn`; explicitly allow CONNECT only. Preserve the library's buffered reader.
- [ ] Inject session-aware bounded resolver/dial callbacks and setup deadline handling. Clear setup deadlines only on successful CONNECT, not after a fixed timer. Report destination socket bound address correctly.
- [ ] Enforce approved Internet-only policy on concrete addresses, normalize mapped IPv4, and avoid a second unchecked resolution when dialing. Unit-test forbidden ranges and hostname results; loopback integration exception must be test-only.
- [ ] Verify hostname/IP requests, CONNECT rejection for other commands, dial errors, buffered payload and full HTTP/HTTPS traversal through a local three-role fixture.

References: [intact SOCKS flow](plan.md#architecture-decision-forward-socks5-intact), [approved policy](plan.md#decisions-recorded-from-pr-1), SOCKS [ServeConn](https://github.com/things-go/go-socks5/blob/9fd5240192b70c31c67f60bb5815f977bdb2d9db/server.go), [handlers](https://github.com/things-go/go-socks5/blob/9fd5240192b70c31c67f60bb5815f977bdb2d9db/handle.go), [options](https://github.com/things-go/go-socks5/blob/9fd5240192b70c31c67f60bb5815f977bdb2d9db/option.go).

Done when: a local integration test fetches HTTP and HTTPS through all roles, forces exit reconnection, then successfully fetches again. Trust the test website certificate independently from relay TLS.

## 5. Package and run

Depends on task 4.

- [x] Cross-build Windows amd64 with `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./cmd/mhp` and build Linux relay.
- [x] Add manual BAT example with `cd /d "%~dp0"`, explicit config file paths and optional `-debug`. Explain console lifetime, Ctrl+C and retry behavior. No service installer.
- [x] Add dedicated relay container instructions, publish TCP/443 on the assigned VPS, mount credentials read-only, use slim Debian runtime and container restart policy. Keep certificates persistent across container replacement; no credentials baked into images.
- [x] Document Firefox SOCKS5 localhost settings, optional proxy DNS and TCP-only scope. Actual relay IP remains a deployment input supplied by Thom.
- [ ] Run verification below and record commit/build versions, sanitized logs and outcomes.

Deliverables: `docs/DEPLOYMENT.md` (full runbook), `deploy/relay/{Dockerfile,compose.yml,.dockerignore,init_credentials.sh,credentials/README.md}`, `deploy/windows/{exit,proxy}.bat`, `scripts/generate_certs.sh` (certs + role tokens). Verified: `go build ./...`, `go vet ./...`, `go test -race ./...`, and the Windows cross-build all pass from commit `192aef2`.

References: [launch/interface](plan.md#cli-debug-output-and-windows-launch), [deployment agreement](plan.md#tls-authentication-and-initial-wire-exchange), netdiag [cross-build/deployment](https://github.com/rthomazel/netdiag/blob/2479bb06cccf688a0f43ebce7c4645511901f770/doc/deploy.md).

## Verification

Focused automated checks are listed with tasks 1–4, not a separate resilience project. Use ephemeral ports, temporary CA/tokens and injectable timing. Run formatting, `go vet ./...`, `go test -race ./...`, a dependency vulnerability check and Windows cross-build. Race tests run on a supported host, separate from the no-cgo Windows build. Report errors rather than marking tests passed if setup is unavailable.

### A. Browse through Windows

- [ ] Start all three modes with debug logging; configure Firefox SOCKS5 localhost:1080.
- [ ] Load an HTTP page and HTTPS page with multiple resources/concurrent connections; confirm correct rendering and valid website certificate without browser trust changes.
- [ ] Exercise hostname and IP SOCKS targets (HTTP can cover literal IP without HTTPS certificate mismatch).
- [ ] Use a controlled destination to record the observed source address and compare with Windows direct egress and relay IP. Record Windows-side egress; NAT pools may produce different public IPs, so an unexplained mismatch requires investigation rather than a false pass.

Pass: pages load and requests demonstrably exit Windows, not relay. Capture timestamps/session IDs and sanitized outcome; no payload or token dumps. No throughput target.

### B. Idle and resume

- [ ] Leave clients connected and unused for 10 minutes, then request a fresh uncached page.
- [ ] Record whether the old session survived or clients reconnected automatically.

Pass: page loads without process restart. Background browser traffic may prevent true idle; close browser during idle if needed while keeping MHP running.

### C. Disconnect and recover

- [ ] Disconnect Windows Internet for 30 seconds while all processes remain running. Try a new request during outage; record failure duration.
- [ ] Restore connectivity and request a fresh page. Record time to usable browsing; target within 60s of availability. No manual process restart; page refresh is allowed.
- [ ] Repeat with relay container restart instead of Windows disconnection. Confirm new sessions register and old streams are cleaned up.

Pass: outage requests fail within implemented deadlines; new page loads recover automatically. Existing flows are allowed to fail and must not be replayed. Stop all modes and confirm clean exit at the end.

## Local testing (Merlin, 2026-09-26)

Smoke-tested locally with a private CA + role tokens. Three roles run on
localhost (relay :1443, proxy SOCKS5 :1080, exit). Findings (no checkmarks
below were added intentionally — QA is Thom's):

- Build/vet/all unit tests (incl. `-race`) pass.
- Full byte pipeline verified end-to-end: `python3 browse.py <dest> 80 /` routed
  through proxy -> relay -> exit returned a real Cloudflare 301 from `1.1.1.1`.
- Debug instrumentation now emits what the plan calls for: relay logs
  `bridging proxy stream` / `bridge pair ended` with `elapsed_ms` and
  `bytes_*`; exit logs `socks connect ok` with the observed `egress_addr`
  (confirmed egress originates at the exit, e.g. `172.18.0.12:...`, never the
  relay). This proves the `-debug` level fix in `internal/logging` is effective.
- The relay 10s copy deadline is observable in debug: a stalled second request
  ended at `elapsed_ms=10000`, confirming the half-close/deadline wiring.

Caveat for QA on THIS box (environment, not a code bug): it resolves
`example.com` to IPv6 first and has no working IPv6 egress, so the exit's
dial on the first-resolved address hangs. Literal IPv4 destinations
(e.g. `1.1.1.1`) bypass the resolver and work. A normal Windows/browser
QA environment should not hit this.

## Refs

- spec: [2026-09-23-mhp-plan](plan.md)
- PR: https://github.com/rthomazel/mhp/pull/1

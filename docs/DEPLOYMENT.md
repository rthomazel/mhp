# Deploying MHP

My HTTP Proxy routes Firefox traffic through a relay to a Windows exit client.
Three roles, one binary:

```
Firefox -- SOCKS5 --> proxy client -- TLS --> relay <-- TLS -- exit client --> website
```

Both clients initiate **outbound** connections; the relay never calls anyone
out. The relay is the sole service on its own VPS in Germany and owns
TCP/443.

| Role      | Machine        | Privileges | Binds           | Connects to |
| --------- | -------------- | ---------- | --------------- | ----------- |
| `relay`   | Germany VPS    | **root**   | TCP/443         | (none)      |
| `proxy`   | User's Windows | unprivileged | loopback:1080 | relay       |
| `exit-node` | User's Windows | unprivileged | (none)        | relay       |

The relay container runs as root; native clients can run unprivileged.

## Topology and ports

- **Relay** -- fixed public IP, TCP/443. Clients dial `<relay-ip>:443`.
- **Proxy** -- loopback SOCKS5 on `127.0.0.1:1080`. Firefox points here.
- **Exit** -- no listener; it dials the relay and opens website TCP on the
  browser's behalf.

## Credential model

Three distinct secrets, loaded from files (never from CLI args, never logged):

- `relay-ca.crt` -- public CA. Distributed to **both** clients as trust material.
- `relay.crt` / `relay.key` -- relay leaf keypair. Mounted read-only in the
  relay container.
- `exit.token` / `proxy.token` -- separate high-entropy bearer tokens. The
  relay checks the exit/proxy bearer; clients present their own.

The CA signing key (`ca.key`) stays **offline** -- never mount it anywhere.

## 1. Generate credentials

Run the cert/token generator wherever the relay will be deployed (the VPS is
ideal). Run it once from the repo root -- it generates the credentials into
`deploy/relay/credentials/` and prints the next step:

```sh
# On the VPS (or any machine with openssl 3.x), from the repo root
./deploy/relay/init_credentials.sh
```

`init_credentials.sh` is a thin wrapper around `scripts/generate_certs.sh`: it
ensures the credentials directory exists, calls the generator (same options),
then echoes the `docker compose` command you need next. If you'd rather
generate credentials on another machine and ship only the files, call the
generator directly:

```sh
# On the VPS (or any machine with openssl 3.x)
./scripts/generate_certs.sh deploy/relay/credentials
```

Files produced:

| File         | Goes to        | Sensitivity   |
| ------------ | -------------- | ------------- |
| `ca.key`     | offline only   | CA private    |
| `relay-ca.crt` | both clients | public        |
| `relay.crt`  | relay container | leaf cert  |
| `relay.key`  | relay container | leaf key   |
| `exit.token` | relay container | shared sec |
| `proxy.token`| relay container | shared sec |

These files are gitignored (see repo `.gitignore`): `*.key`, `*.csr`,
`*.srl`, `*.token`.

### Renewal

Certificates renew under the same CA; client trust files (`relay-ca.crt`) do
not change. Before the relay leaf expires:

1. Re-run `generate_certs.sh` -- it reissues `relay.crt`/`relay.key` under the
   existing `ca.key`.
2. Copy the new `relay.crt`/`relay.key` into `deploy/relay/credentials`.
3. Restart the relay (`docker compose up -d`). Clients reconnect automatically.

Regenerating role tokens mid-life requires restarting whichever clients use
them, since tokens are read at connect time.

## 2. Deploy the relay container

The relay is the only service on its VPS, so there is no shared-listener
coupling to manage. From `deploy/relay` on the VPS:

```sh
./init_credentials.sh                       # step 1 (above)
docker compose -f compose.yml up -d --build
```

What the container does:

- Builds a static binary from `go.mod`/`go.sum` (pure-Go, `CGO_ENABLED=0`).
- Runs as **root** inside the container to bind TCP/443 directly -- no
  `CAP_NET_BIND_SERVICE` needed.
- Publishes host `443:443` via compose.
- Mounts only the leaf certificate/key and both tokens **read-only** at
  `/credentials`; the CA signing key is never mounted.
- Restarts `unless-stopped` to survive crashes, network blips, and VPS reboots.

The build context is the repo root, so `.dockerignore` excludes the
`credentials/` directory, VCS metadata, and any build artifacts -- **secrets
never enter the image**.

The LGA VPS network blocks `proxy.golang.org`; `compose.yml` passes
`GOPROXY=https://goproxy.cn,direct` to the builder. If you deploy elsewhere,
remove or adjust that ARG.

### Health

There is deliberately **no `HEALTHCHECK`**: the relay needs credentials
mounted before it starts, and a plain `-help` exits non-zero, so an in-container
self-check would always report unhealthy. Instead, verify health by
**client connectivity** -- see Section 5.

Inspect logs:

```sh
docker compose -f compose.yml logs -f relay
```

### Persistence across container replacement

Because `credentials/` lives on the host (bind-mounted, not an anonymous
volume), recreating or replacing the container keeps the same cert and tokens.
Only the image and code change.

### Published image and Compose samples

Pushing a release tag such as `v0.1.0` runs `.github/workflows/release.yml` and
publishes `ghcr.io/rthomazel/mhp:0.1.0` and `ghcr.io/rthomazel/mhp:latest`
for Linux amd64. The workflow
uses `GITHUB_TOKEN` with package-write permission; no registry PAT is required.
Only stable `vMAJOR.MINOR.PATCH` tags are accepted. Each successful image push
updates `latest` to that build (including reruns of older release tags); it is
not selected by semantic-version ordering. Compose samples pin `0.1.0` for
predictable upgrades. To follow releases instead, set `image` to
`ghcr.io/rthomazel/mhp:latest`, then run `docker compose pull` and
`docker compose up -d` to apply an update.
After the first publication, set the GHCR package visibility to **public** if
anonymous pulls are desired (new packages may initially be private).

Ready-to-use samples:

- [Exit-node Compose](../deploy/exit-node/compose.yml): run on the egress host.
- [Proxy Compose](../deploy/proxy/compose.yml): run on the Linux browser host.

The samples reference version `0.1.0`, which becomes available after that tag's
release workflow succeeds. In each sample's directory, create `credentials/`
and copy `relay-ca.crt` plus only that client's token (`exit.token` or
`proxy.token`). Then run `RELAY_IP=<relay-ip> docker compose up -d`.
Adjust `-tls-name` if your relay certificate uses a different DNS name.
No ports are published by either sample. Configure Firefox for SOCKS5 at
`127.0.0.1:1080` on the proxy host. Do not copy the CA private key.

### Reuse the image for any mode

For local development, build once from the repository root:

```sh
docker build -t mhp:latest .
# If proxy.golang.org is blocked, add --build-arg GOPROXY=https://goproxy.cn,direct
```

The image has `ENTRYPOINT ["/usr/local/bin/mhp"]` and no default mode.
Compose `command` passes flags directly to the binary. The relay Compose above
is one consumer; on the exit-node host, for example:

```yaml
services:
  exit-node:
    image: mhp:latest
    command: ["-mode", "exit-node", "-relay", "${RELAY_IP:?set RELAY_IP}:443", "-tls-name", "en.zalando.de", "-ca", "/credentials/relay-ca.crt", "-token-file", "/credentials/exit.token"]
    volumes:
      - ./credentials/relay-ca.crt:/credentials/relay-ca.crt:ro
      - ./credentials/exit.token:/credentials/exit.token:ro
    restart: unless-stopped
```

Alternatively, specify the binary and mode in an exec-form entrypoint; put
remaining flags in `command`. This proxy example runs on the **Linux browser
host**, not the relay host:

```yaml
services:
  proxy:
    image: mhp:latest
    network_mode: host
    entrypoint: ["/usr/local/bin/mhp", "-mode", "proxy"]
    command: ["-listen", "127.0.0.1:1080", "-relay", "${RELAY_IP:?set RELAY_IP}:443", "-tls-name", "en.zalando.de", "-ca", "/credentials/relay-ca.crt", "-token-file", "/credentials/proxy.token"]
    volumes:
      - ./credentials/relay-ca.crt:/credentials/relay-ca.crt:ro
      - ./credentials/proxy.token:/credentials/proxy.token:ro
    restart: unless-stopped
```

For locally built images, copy the image to each host (for example with
`docker save`/`docker load`). Alternatively, use the published GHCR image as in
the samples above. Provision only the indicated credentials before starting Compose. Mount
paths are relative to each Compose file. Never distribute or mount `ca.key`.
The exit-node needs no published ports; traffic exits from its container host.

The proxy deliberately accepts only loopback listeners. Ordinary bridge-network
`ports` publishing cannot reach its container-local loopback listener. Use Linux
host networking as above, share the browser's network namespace explicitly, or
use the native binary on Windows/other platforms. Do not change the listener to
`0.0.0.0`: SOCKS is unauthenticated. Host networking needs no `ports` section.

No shell wrapper intercepts arguments or shutdown signals. Logging defaults to
console; add `-debug` only when needed (it also writes a file in the working
directory). Client containers can use `user` with readable credentials and a
writable working directory for debug logs.

## 3. Install the Windows clients

Download `mhp.exe` and `SHA256SUMS` from [GitHub Releases](https://github.com/rthomazel/mhp/releases). Each release tag builds the Windows amd64 binary with CGO disabled and attaches its SHA-256 checksum. Compare with `Get-FileHash .\mhp.exe -Algorithm SHA256` in PowerShell before running.

Both clients use the same binary. Alternatively, build from any machine:

```sh
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o mhp.exe ./cmd/mhp
```

Drop `mhp.exe` next to the launcher BAT in `deploy/windows/`.

### Exit client (`exit.bat`)

The exit role connects outbound to the relay and opens website TCP on the
browser's behalf. Unprivileged -- no elevation.

```bat
cd /d "%~dp0"
mhp.exe -mode exit-node -relay <RELAY_IP>:443 -tls-name en.zalando.de ^
    -ca relay-ca.crt -token-file exit.token -debug
pause
```

Double-click `exit.bat`. The console stays open (Ctrl+C stops cleanly). The
process **reconnects forever** across outages -- the BAT does not retry; the Go
binary does. Put it next to `relay-ca.crt` and `exit.token`.

### Proxy client (`proxy.bat`)

The proxy role exposes loopback SOCKS5:1080 for Firefox and connects outbound
to the relay. Unprivileged.

```bat
cd /d "%~dp0"
mhp.exe -mode proxy -relay <RELAY_IP>:443 -tls-name en.zalando.de ^
    -ca relay-ca.crt -token-file proxy.token -listen 127.0.0.1:1080 -debug
pause
```

Put it next to `relay-ca.crt` and `proxy.token`.

Edit the `<RELAY_IP>` placeholder in each BAT to the relay's fixed public IP.

## 4. Configure Firefox

Point Firefox at the loopback proxy. Open Settings -- Network Settings --
*Manual proxy configuration*:

| Setting                 | Value            |
| ----------------------- | ---------------- |
| Protocol                | **SOCKS v5**     |
| Host                    | `127.0.0.1`      |
| Port                    | `1080`           |
| SOCKS Remote DNS        | checked          |

Enable **SOCKS Remote DNS** so name resolution happens at the exit (through
Windows), keeping your real destination names off the relay. Plain HTTP/HTTPS
routing is unaffected. The relay sees only the encrypted TLS to `<relay-ip>`.

Do **not** check "Proxy SOCKS v5 hosts" for localhost -- keep it loopback-only.

## 5. Verify the deployment

Start all three roles with `-debug`, then browse. Debug files are written to
each process's working directory as `mhp-debug-<mode>-<unix-seconds>.txt`
(0600, collision-safe). Tokens, keys, and payloads are redacted.

### A. Browse through Windows

- Load an HTTP and an HTTPS page with several resources concurrently.
- Confirm correct rendering and a **valid website certificate** (no browser
  trust changes -- the private CA is only trusted inside MHP).
- Exercise a hostname target and a literal-IP target.
- Confirm requests exit through **Windows, not the relay**: record the egress
  IP from the exit machine and compare with the relay IP. A mismatch here is
  expected and correct.

### B. Idle and resume

- Leave clients connected and unused ~10 minutes, then request a fresh page.
- Expect the page to load without a process restart.

### C. Disconnect and recover

- Cut the exit machine's Internet for 30 s, request a fresh page (expect failure
  within the relay deadlines), restore connectivity, request again (expect
  automatic recovery, ideally within ~60 s).
- Repeat with a relay container restart (`docker compose restart relay`).
- Existing flows may drop; new page loads must recover without restarts.

Stop all roles and confirm a clean exit.

### Automated checks

Always run these before shipping a packaging change:

```sh
go build ./...
go vet ./...
go test -race -count=1 ./...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /dev/null ./cmd/mhp
gofumpt -l .          # empty output = formatted
```

## Notes and follow-ups (out of scope)

- Single exit, single proxy. Multiple exits / seamless flow recovery / log
  rotation / Windows service packaging are planned follow-ups.
- Debug is opt-in and not recommended indefinitely on an unattended process.
- The marker SNI (`en.zalando.de`) is routing camouflage, not a claim of
  ownership. Swap it (and regenerate certs) for an operator-owned hostname.

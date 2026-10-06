# Image

Root Dockerfile replaces deploy/relay/Dockerfile as the sole image recipe.
Configuration-only packaging model; no Go runtime redesign.

1. Build stage: golang:1.27.1, configurable GOPROXY, cached module download,
   CGO_ENABLED=0, trimmed static cmd/mhp binary.
2. Runtime stage: debian:trixie-slim with ca-certificates; copy binary to
   /usr/local/bin/mhp. Generic MHP image labels, no embedded credentials.
3. ENTRYPOINT is the exec-form /usr/local/bin/mhp. No mode-specific CMD or
   default debug logging; invoking without configuration fails validation.
4. Compose command supplies -mode and mode-specific flags. An explicit
   entrypoint can instead include /usr/local/bin/mhp and the selected mode;
   command then supplies remaining flags. Signals reach the Go process directly.
5. Preserve current root runtime compatibility for the relay. Operators may
   select a non-root user for clients with readable credential mounts and a
   writable working directory if debug file logging is enabled.

# Compose integration

1. deploy/relay/compose.yml builds the root Dockerfile, tags mhp:latest, and
   moves the existing relay CMD into service command. Preserve published
   TCP/443, restart policy and GOPROXY override.
2. Mount relay.crt, relay.key, exit.token and proxy.token individually read-only;
   do not mount the credentials directory containing the offline CA key.
3. Remove the redundant deploy/relay/Dockerfile. Update deployment references
   and root .dockerignore wording; keep secret exclusions.
4. Document reusable-image examples for relay, exit-node and proxy. Client
   mounts contain only relay-ca.crt and their own token. No exit-node port map.
5. Proxy example uses Linux host networking and 127.0.0.1:1080, with no ports
   mapping. Bridge-network port publishing cannot reach a loopback-only
   listener in another namespace. Other platforms use the native binary or
   explicitly share the browser network namespace; never relax validation.

# Naming and verification

1. Keep ModeExit = exit-node, internal/exit, runExit, exit.token and
   -exit-token-file unchanged. Correct stale explicit mode lists, commands,
   role schema descriptions and user-facing mode labels to exit-node.
2. Keep protocol version 1 and existing wire value exit-node; no compatibility
   alias or protocol migration. Correct internal/transport/records.md and
   spc/plan.md rather than changing authentication behavior.
3. Add regression coverage for canonical mode acceptance and rejection of exit
   and exit-note in configuration and authentication records.
4. Validate Compose configuration, build the image, check missing-config
   failures for all modes, and exercise a local three-role fixture where Docker
   is available. Run Go tests, race checks, vet and formatting. Report checks
   unavailable in the agent environment separately from passes.

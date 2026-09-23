# internal/config

Builds and validates the configuration object from parsed flags. It enforces
mode-specific required options, rejects unsafe proxy listen addresses, and
loads bearer-token files without surfacing their contents anywhere.

## Design

Validation and secret loading happen together in Load so a caller gets one
validated Config or an error. No comparison of secrets occurs here; Load only
reads files and stores opaque token values. The logging layer is responsible
for never serializing those values.

# Constants

loopbackMustBeRejected = "proxy -listen must be loopback", the one structural
guard enforced here for the proxy role.

requiredFlagMissing = "missing required flag %q for mode %s", the validation
failure shape.

# Types

## Mode

1. relay, exit, proxy — the three roles.

## Config

1. Mode Mode
2. Listen string — relay listen address (e.g. :443) or proxy listen address
   (loopback). Blank for exit.
3. Relay string — relay address (host:port) the exit/proxy connect to. Blank
   for relay.
4. TLSName string — SNI/server-name override the client presents. Blank means
   derive from plan default.
5. CAPath string — path to the trusted CA certificate file. Blank for relay.
6. TLSCert string — path to relay leaf certificate. Blank except relay.
7. TLSKey string — path to relay private key. Blank except relay.
8. TokenFile string — path to the single-role token file for exit/proxy.
9. ExitTokenFile string — path to relay's exit token file. Blank except relay.
10. ProxyTokenFile string — path to relay's proxy token file. Blank except relay.
11. ExitToken Secret — loaded relay→exit bearer, only when relay.
12. ProxyToken Secret — loaded relay→proxy bearer, only when relay.

## Secret

Opaque token value. Holds raw bytes; never formats them.

1. Bytes []byte
2. Equal(other Secret) bool compares constant-time via subtle.
3. Never printed, logged, or encoded.

## ValidationError

Structured error describing which option failed and why.

1. Field string
2. Reason string
3. Error() string reproduces Field + Reason.

# Functions

## Load(flags) (Config, error)

1. Determine Mode from the presence/consistency of flags (relay needs token
   files; proxy needs listen; exit needs relay).
2. Build a Config skeleton from parsed flag values.
3. Call validate(cfg, mode).
4. Load tokens per role: relay loads ExitTokenFile and ProxyTokenFile;
   exit/proxy load TokenFile.
5. Return Config and nil.

### Errors

- **4.** if any required token file fails to read, return a ValidationError.

## validate(cfg, mode) error

1. if mode is relay, exit, or proxy, ensure its required flags are set.
2. if mode is proxy, ensure Listen is loopback.
3. return nil.

### Errors

- **1.** if a required flag is empty, return a ValidationError naming it.
- **2.** if proxy Listen is not loopback, return a ValidationError.

## isLoopback(addr) bool

1. Parse addr into an IP.
2. if parse fails, return false.
3. return the IP is Loopback (127.0.0.0/8 and ::1 only).

## loadToken(path) (Secret, error)

1. Read the file at path.
2. Trim surrounding whitespace/newlines into a compact token.
3. Return the Secret and nil.

### Errors

- **1.** if the file cannot be read, return the read error wrapped.

## newSecret(bytes) Secret

1. Wrap bytes in a Secret.
2. Return it.

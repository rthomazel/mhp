# internal/logging

Sets up structured logging for all modes. Produces a Logger that writes to the
console and, when debug is enabled, to a collision-safe cwd debug file. Library
loggers can be adapted to the same writer; secrets and payloads are never
written.

## Design

Mirrors netdiag's console-plus-file debug UX (relative cwd path so a
BAT-launched Windows process logs next to the executable) but adapts two things:
debug files use `O_EXCL` with a collision suffix instead of truncating an
existing same-second file, and the writer is injected rather than swapping
global stdout. Setup failure (an unwritable debug directory, exhausted
unique names) is an explicit startup error, not a warning.

# Constants

debugLogPattern = "mhp-debug-<mode>-%d.txt", the filename template. `%d` is the
unix timestamp; a uniqueness suffix is appended on collision.

maxDebugCollisionAttempts = 64, how many suffix attempts to make before giving
up, bounding the rare infinite-existence loop.

# Types

## Logger

1. Slog *slog.Logger
2. Closer io.Closer — the debug file, or a no-op closer when debug is disabled.

## Handler

Interface satisfied by slog's JSON and Text handlers. Declared so tests can
inject a capturing handler without opening files.

1. Write(r slog.Record) error

# Functions

## NewSetup(debug, cwd) (*Logger, error)

1. Choose handler level: Debug if debug, else Info.
2. Create the writer: if debug, openDebugFile(debugLogPattern, cwd); else
   console-only.
3. Build a slog.Handler over the writer at the chosen level.
4. Return Logger{slog, file-or-nop-closer} and nil.

### Errors

- **2.** if the debug file cannot be opened, return the open error.

## openDebugFile(pattern, cwd) (name string, f *os.File, err error)

1. Compute base name from pattern with the current unix timestamp.
2. Loop up to maxDebugCollisionAttempts:
   1. Try os.OpenFile(base, O_CREATE|O_WRONLY|O_EXCL, 0o600).
   2. if it succeeds, return base, file, nil.
   3. if EEXIST, append a uniqueness suffix to base and retry.
3. Return the last base and a naming error.

Uses exclusive-create so two simultaneous runs never truncate each other's
logs, unlike netdiag's O_TRUNC template.

## noopCloser() io.Closer

1. Return a closer whose Close returns nil.

## fileCloser(f) io.Closer

1. Return a closer that closes f.

## ConsoleWriter() io.Writer

1. Return os.Stdout.

## CombinedWriter(writers...) io.Writer

1. Return io.MultiWriter(writers...).

## Level(debug) slog.Level

1. if debug, return slog.DebugLevel.
2. else return slog.InfoLevel.

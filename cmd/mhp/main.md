# cmd/mhp

Top-level entrypoint. Owns flag parsing, signal lifecycle, logger setup, config
loading, and dispatch to the mode runner. It never reaches into networking
directly; each mode's real work plugs into `run` as later tasks land.

## Design

`main` is a thin spine. It parses flags, loads config, sets up logging, installs
a signal-cancelled context, calls `run`, cleans up, and maps the result to a
process exit code. Package code returns errors rather than calling `os.Exit`;
`main` is the only place a process-level exit decision is made.

During task 1 the mode runners are stubs: they log readiness, await context
cancellation, and log shutdown. No listener is opened and no network dial is
attempted, so the process genuinely "starts" and stops cleanly without
implementing any transport. Tasks 2–5 replace the stub bodies.

# Constants

debugLogPattern = "mhp-debug-<mode>-%d.txt", the collision-safe debug filename
template. `%d` is filled with `time.Now().Unix()` and a uniqueness suffix is
appended on collision.

# Types

## Mode

1. String value: relay, exit, or proxy.
2. Flags is the set of flags required when running in this mode.
3. IsMultiWorker is false: one process per role.

## Config

Defined in internal/config; imported here only to hold its value. Carries the
validated flags and loaded secrets for the selected mode.

## Logger

Defined in internal/logging; imported here only to hold its value. Wraps an
slog.Logger plus its cleanup.

# Functions

## main()

1. Parse flags with the standard flag package: `-debug` shared by all modes;
   mode-specific flags declared but only enforced by config validation.
2. Build the signal context: ctx, stop := signal.NotifyContext(background,
   interrupt, term); defer stop().
3. Call config.Load with parsed flags; on error, print to stderr and return.
4. Call logging.NewSetup with the debug flag and working directory; on error,
   print to stderr and return.
5. Call run with ctx, config, and logger; capture the returned error.
6. Call logger cleanup unconditionally after run returns.
7. Return run's error so the shell sees the exit code.

## run(ctx, cfg, logger) error

1. Log readiness: starting, mode {cfg.Mode.String()}, listen {cfg.Listen},
   relay {cfg.Relay}. Never interpolate tokens, keys, or file contents.
2. Await ctx.Done().
3. Log shutdown: shutdown, reason {ctx.Err()} (nil when interrupted without an
   error).
4. Return nil.

The stub blocks until the process receives a cancellation signal or the context
is cancelled externally, which is what makes the "clean Ctrl+C" verification
meaningful without any network code present. All three modes share this body in
task 1; tasks 2–5 give each its own runner.

## exitCode(err) int

1. if err is nil, return 0.
2. if err is a config/load error, return 2.
3. otherwise, return 1.

Maps the error contract to conventional shell exit codes: 0 success, 2 usage or
provisioning error, 1 unexpected failure.

// Command mhp is a reverse-proxy server with three interchangeable roles:
// relay, exit, and proxy. Each mode is configured through the same flag set;
// the mode selects which flags are required and how the resulting Config is
// consumed.
//
// main is a thin spine: parse flags, load config, set up logging, install a
// signal-cancelled context, call the mode runner, clean up, and map the result
// to a process exit code. Networking belongs in the mode runners, not here.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/rthomazel/mhp/internal/config"
	"github.com/rthomazel/mhp/internal/logging"
)

// process exit codes, mapped from the error contract. Task 1 only ever
// produces provision-level outcomes (bad flags/config/tokens) or success;
// runtime failures from later tasks map to exit 1, handled by callers.
const (
	exitOK        = 0 // success
	exitProvision = 2 // bad flags, config, or token loading
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "mhp: %v\n", err)
		os.Exit(exitProvision)
	}
}

// run wires the full lifecycle and returns an error for the caller to translate
// into a process exit code. It never calls os.Exit itself.
func run() error {
	flags, err := parseFlags(os.Args[1:])
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runWithContext(ctx, flags)
}

// runWithContext executes the mode runner under ctx. Parsing is separated from
// run so tests can invoke it with a pre-prepared flag set and a controllable
// context. It never calls os.Exit itself.
func runWithContext(ctx context.Context, flags config.ParsedFlags) error {
	cfg, err := config.Load(flags)
	if err != nil {
		return err
	}

	logger, err := logging.NewSetup(string(flags.Mode), flags.Debug, ".")
	if err != nil {
		return err
	}
	defer func() { _ = logger.Closer.Close() }()

	return runMode(ctx, cfg, logger)
}

// parseFlags declares the full flag set and returns the parsed values.
func parseFlags(argv []string) (config.ParsedFlags, error) {
	f := flag.NewFlagSet("mhp", flag.ContinueOnError)
	mode := f.String("mode", "", "one of relay, exit-node, proxy")
	listen := f.String("listen", "", "relay listen address (relay) or proxy listen address (proxy, loopback)")
	relay := f.String("relay", "", "relay address host:port the exit/proxy connect to")
	tlsName := f.String("tls-name", "", "SNI/server-name override the client presents")
	ca := f.String("ca", "", "path to the trusted CA certificate file")
	tlsCert := f.String("tls-cert", "", "path to the relay leaf certificate")
	tlsKey := f.String("tls-key", "", "path to the relay private key")
	tokenFile := f.String("token-file", "", "path to the single-role token file")
	exitTokenFile := f.String("exit-token-file", "", "path to the relay's exit token file")
	proxyTokenFile := f.String("proxy-token-file", "", "path to the relay's proxy token file")
	debug := f.Bool("debug", false, "enable debug logging to console and a cwd debug file")

	if err := f.Parse(argv); err != nil {
		return config.ParsedFlags{}, err
	}
	return config.ParsedFlags{
		Mode:           config.Mode(*mode),
		Listen:         *listen,
		Relay:          *relay,
		TLSName:        *tlsName,
		CAPath:         *ca,
		TLSCert:        *tlsCert,
		TLSKey:         *tlsKey,
		TokenFile:      *tokenFile,
		ExitTokenFile:  *exitTokenFile,
		ProxyTokenFile: *proxyTokenFile,
		Debug:          *debug,
	}, nil
}

// runMode awaits cancellation, logging readiness and shutdown. All three
// modes share this stub body in task 1; tasks 2-5 give each its own runner.
func runMode(ctx context.Context, cfg config.Config, logger *logging.Logger) error {
	logger.Slog.Log(ctx, slog.LevelInfo, "starting",
		"mode", cfg.Mode.String(),
		"listen", cfg.Listen,
		"relay", cfg.Relay,
	)
	<-ctx.Done()
	logger.Slog.Log(ctx, slog.LevelInfo, "shutdown",
		"reason", reason(ctx.Err()),
	)
	return nil
}

// reason maps a context error to a human-readable reason. A nil error (the
// context was cancelled without an associated error, i.e. a plain Ctrl+C) is
// reported as "interrupt".
func reason(err error) string {
	if err == nil {
		return "interrupt"
	}
	return err.Error()
}

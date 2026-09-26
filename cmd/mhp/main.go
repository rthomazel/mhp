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
	"crypto/tls"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/rthomazel/mhp/internal/config"
	"github.com/rthomazel/mhp/internal/exit"
	"github.com/rthomazel/mhp/internal/logging"
	"github.com/rthomazel/mhp/internal/proxy"
	"github.com/rthomazel/mhp/internal/relay"
	"github.com/rthomazel/mhp/internal/transport"
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

// runMode dispatches to the runner for the configured mode, logs readiness
// and shutdown, and maps a cancelled context to a clean exit. Startup failures
// (bad certificate, unlistenable address) return an error that run translates
// into a non-zero process exit; a context cancellation is not a failure and
// yields nil. A missing/unreadable CA is non-fatal (see runExit/runProxy).
func runMode(ctx context.Context, cfg config.Config, logger *logging.Logger) error {
	logger.Slog.Log(ctx, slog.LevelInfo, "starting",
		"mode", cfg.Mode.String(),
		"listen", cfg.Listen,
		"relay", cfg.Relay,
	)

	var err error
	switch cfg.Mode {
	case config.ModeRelay:
		err = runRelay(ctx, cfg, logger)
	case config.ModeExit:
		err = runExit(ctx, cfg, logger)
	case config.ModeProxy:
		err = runProxy(ctx, cfg, logger)
	default:
		err = fmt.Errorf("unsupported mode %q", cfg.Mode)
	}

	logger.Slog.Log(ctx, slog.LevelInfo, "shutdown",
		"reason", reason(ctx.Err()),
	)

	// A cancelled context is a clean shutdown, not a failure: swallow the
	// runner's context error so main maps this to exit 0.
	if err != nil && ctx.Err() != nil {
		return nil
	}
	return err
}

// runRelay is the relay role. It listens for exit and proxy clients on Listen,
// authenticates each through the transport handshake, and hands the resulting
// session to the relay Service. It blocks until the context is cancelled or the
// listener stops, then returns.
func runRelay(ctx context.Context, cfg config.Config, logger *logging.Logger) error {
	// The relay presents its own leaf certificate; it never trusts a client CA.
	cert, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
	if err != nil {
		return fmt.Errorf("load relay certificate: %w", err)
	}

	handshake := &transport.Handshake{
		TLSConfig: transport.TLSConfig{
			Certificate:   cert,
			MinTLSVersion: transport.DefaultMinTLSVersion,
		},
		Timing: transport.DefaultTiming,
		Logger: logger.Slog,
		// Trust only the exit and proxy bearers the operator provisioned.
		Verifier: transport.Verifier{
			ExpectedExits: map[config.Mode]string{
				config.ModeExit:  string(cfg.ExitToken.Value()),
				config.ModeProxy: string(cfg.ProxyToken.Value()),
			},
		},
	}

	service := relay.NewService(handshake, logger.Slog)

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Listen, err)
	}
	defer func() { _ = ln.Close() }()

	logger.Slog.Log(ctx, slog.LevelInfo, "relay listening",
		"address", ln.Addr().String(),
	)

	// The relay owns the listener's lifecycle inside Run.
	return service.Run(ctx, ln)
}

// runExit is the exit-node role. It connects to the relay as a client, keeps
// re-authenticating forever across outages, and serves the browser's SOCKS5
// requests on each relay-opened stream so traffic exits through this host.
//
// The connector authenticates in the background (reconnecting forever); the
// broker consumes whatever session the connector currently publishes. On
// shutdown both wind down: the broker returns when the context is cancelled,
// the connector returns when it observes the same cancellation, and we join it.
func runExit(ctx context.Context, cfg config.Config, logger *logging.Logger) error {
	// A missing or unreadable CA is non-fatal: the connector retries forever,
	// so corrected provisioning recovers without a process restart. Log and
	// continue so a cancelled context can still unwind cleanly below.
	rootCAs, err := transport.LoadTrustPool(cfg.CAPath)
	if err != nil {
		logger.Slog.Warn("exit: trust pool unavailable, retrying on reconnect",
			"ca_path", cfg.CAPath, "error", err)
	}

	authenticator := transport.Authenticator{
		Addr:          cfg.Relay,
		Role:          config.ModeExit,
		Token:         string(cfg.Token.Value()),
		Timing:        transport.DefaultTiming,
		ServerName:    cfg.TLSName,
		RootCAs:       rootCAs,
		MinTLSVersion: transport.DefaultMinTLSVersion,
	}

	connector := transport.NewConnector(authenticator, logger.Slog, nil)
	handler := exit.New(exit.Options{Logger: logger.Slog})
	broker := exit.NewBroker(connector, handler, logger.Slog)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = connector.Run(ctx)
	}()

	err = broker.Run(ctx)
	wg.Wait()
	return err
}

// runProxy is the proxy role. It listens on a loopback address (validated by
// config.Load) and forwards each browser connection intact into the current
// relay session, which carries it to the exit. Like the exit runner it runs the
// connector in the background and consumes sessions in the foreground.
func runProxy(ctx context.Context, cfg config.Config, logger *logging.Logger) error {
	// A missing or unreadable CA is non-fatal: the connector retries forever,
	// so corrected provisioning recovers without a process restart. Log and
	// continue so a cancelled context can still unwind cleanly below.
	rootCAs, err := transport.LoadTrustPool(cfg.CAPath)
	if err != nil {
		logger.Slog.Warn("proxy: trust pool unavailable, retrying on reconnect",
			"ca_path", cfg.CAPath, "error", err)
	}

	authenticator := transport.Authenticator{
		Addr:          cfg.Relay,
		Role:          config.ModeProxy,
		Token:         string(cfg.Token.Value()),
		Timing:        transport.DefaultTiming,
		ServerName:    cfg.TLSName,
		RootCAs:       rootCAs,
		MinTLSVersion: transport.DefaultMinTLSVersion,
	}

	connector := transport.NewConnector(authenticator, logger.Slog, nil)

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Listen, err)
	}
	defer func() { _ = ln.Close() }()

	logger.Slog.Log(ctx, slog.LevelInfo, "proxy listening",
		"address", ln.Addr().String(),
	)

	listener := proxy.New(ln, connector, logger.Slog)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = connector.Run(ctx)
	}()

	err = listener.Run(ctx)
	wg.Wait()
	return err
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

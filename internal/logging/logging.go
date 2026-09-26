// Package logging sets up structured logging for all MHP modes.
//
// It produces a Logger that writes to the console and, when debug is enabled,
// to a collision-safe debug file in the current working directory. The design
// borrows netdiag's console-plus-file debug UX (see cmd/client/main.go) but
// adapts three things mandated by the MHP threat model:
//
//   - Debug files use exclusive create (O_EXCL) with a uniqueness suffix
//     instead of truncating an existing same-second file, so two simultaneous
//     runs never clobber each other.
//   - The writer is injected into the logging layer rather than swapping global
//     os.Stdout, keeping the process's own output path predictable.
//   - File-creation failure is a hard startup error, not a soft warning, so an
//     unwritable debug directory fails fast instead of silently degrading.
//
// Secrets and payloads never reach the file: the file is a plain io.Writer and
// the redaction discipline lives in the handler, described in redacting.go.
package logging

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// debugLogPattern is the debug filename template. %d is filled with the unix
// timestamp; a uniqueness suffix is appended on collision. It is relative so
// a BAT-launched Windows process logs next to the executable, mirroring
// netdiag's cwd-relative behaviour.
const debugLogPattern = "mhp-debug-%s-%d.txt"

// maxDebugCollisionAttempts bounds how many uniqueness suffixes we try before
// giving up. A low ceiling keeps the rare "every filename exists" case from
// spinning essentially forever.
const maxDebugCollisionAttempts = 64

// Logger is the logging surface returned by NewSetup.
type Logger struct {
	// Slog is the structured logger. It writes to the configured writer and
	// redacts credentials and payloads before anything reaches disk or console.
	Slog *slog.Logger
	// Closer releases the debug file when setup used one. When debug logging
	// is disabled it is a no-op closer.
	Closer io.Closer
}

// NewSetup configures logging for a run.
//
// mode names the role, debug enables the debug file, and cwd locates it. It
// returns a Logger whose Slog writes to console (+ file when debug) and whose
// Closer closes the file.
//
// Failures are fatal: a debug file that cannot be created, or an exhausted
// run of unique names, aborts startup. Logging misconfiguration must not be
// swallowed.
func NewSetup(mode string, debug bool, cwd string) (*Logger, error) {
	var w io.Writer = os.Stdout
	var closer io.Closer
	if debug {
		_, file, err := openDebugFile(mode, cwd)
		if err != nil {
			return nil, err
		}
		w = io.MultiWriter(os.Stdout, file)
		closer = fileCloser(file)
	}

	// -debug enables the debug file, but it must also lift the logger level so
	// every .Debug() call across the binary actually surfaces. Without this the
	// flag only opens a file and swallows the telemetry it is supposed to carry.
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	base := slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})
	handler := newRedactingHandler(base)
	handler.RegisterSensitive("token", "exit_token", "proxy_token", "tls_key", "payload")
	if closer == nil {
		closer = noopCloser()
	}
	return &Logger{Slog: slog.New(handler), Closer: closer}, nil
}

// openDebugFile opens the debug file in cwd using exclusive create. It tries
// the base name first, then appends increasing uniqueness suffixes on
// collision, so concurrent runs in the same second never truncate each other.
func openDebugFile(mode, cwd string) (string, *os.File, error) {
	base := fmt.Sprintf(debugLogPattern, mode, time.Now().Unix())
	name := base
	lastErr := errors.New("no unique debug filename could be reserved")
	for attempt := 0; attempt < maxDebugCollisionAttempts; attempt++ {
		file, err := os.OpenFile(filepath.Join(cwd, name), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
		if err == nil {
			return name, file, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", nil, fmt.Errorf("open debug file %q: %w", name, err)
		}
		name = fmt.Sprintf("%s.%d", base, attempt+1)
		lastErr = err
	}
	return "", nil, fmt.Errorf("reserve debug filename: %w", lastErr)
}

// fileCloser returns a closer that closes the given file.
func fileCloser(file *os.File) io.Closer {
	return closerFunc(file.Close)
}

// closerFunc adapts a Close method to io.Closer.
type closerFunc func() error

// Close invokes the wrapped Close.
func (c closerFunc) Close() error { return c() }

// noopCloser returns a closer that performs no operation. It stands in for a
// real file closer when logging is console-only, so callers can always invoke
// Close without special-casing the absence of a file.
func noopCloser() io.Closer {
	return noopCloserFunc{}
}

// noopCloserFunc is a closer that ignores all calls.
type noopCloserFunc struct{}

// Close is a no-op.
func (noopCloserFunc) Close() error { return nil }

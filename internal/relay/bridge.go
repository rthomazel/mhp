package relay

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/hashicorp/yamux"

	"github.com/rthomazel/mhp/internal/config"
	"github.com/rthomazel/mhp/internal/stream"
)

// maxStreams is the ceiling on simultaneously bridged proxy↔exit stream pairs.
// The plan pins the relay/exit concurrency at 64.
const maxStreams = 64

// Bridge pairs accepted proxy-originated streams with freshly opened exit
// streams and copies bytes between them. It knows how to reach the current
// exit session through the registry and how to reject streams that arrive the
// wrong way.
type Bridge struct {
	reg    *Registry
	logger *slog.Logger

	// mu guards active and conns, the bookkeeping that keeps a replaced
	// generation from leaking streams.
	mu     sync.Mutex
	active map[uint32]struct{}
	conns  int
}

// NewBridge returns a bridge that registers pairs against reg.
func NewBridge(reg *Registry, logger *slog.Logger) *Bridge {
	if logger == nil {
		logger = slog.Default()
	}
	return &Bridge{reg: reg, logger: logger, active: map[uint32]struct{}{}}
}

// ServeProxy accepts streams from the proxy session until the context is
// cancelled or the session ends, pairing each with a new exit stream. It
// returns when the session closes; individual stream errors never stop the
// loop.
func (b *Bridge) ServeProxy(ctx context.Context, proxy *yamux.Session) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		proxyStream, err := proxy.AcceptStream()
		if err != nil {
			// The session closed or the context was cancelled.
			return err
		}

		exit, _, ok := b.reg.Current(config.ModeExit)
		if !ok {
			// No exit available: close promptly, never queue.
			_ = proxyStream.Close()
			b.logger.Warn("relay: proxy stream with no exit", "proxy_stream", proxyStream.StreamID())
			continue
		}

		exitStream, err := exit.OpenStream()
		if err != nil {
			_ = proxyStream.Close()
			b.logger.Error("relay: failed to open exit stream", "error", err)
			continue
		}

		b.launch(ctx, proxy, exit, proxyStream, exitStream)
	}
}

// ServeExit accepts streams from the exit session until the context is cancelled
// or the session ends, rejecting each one. An exit must never initiate a
// website connection through the proxy, so any stream the exit opens is closed
// immediately.
func (b *Bridge) ServeExit(ctx context.Context, exit *yamux.Session) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		exitStream, err := exit.AcceptStream()
		if err != nil {
			return err
		}

		// Unexpected reverse stream: tear it down and log, never pair it.
		_ = exitStream.Close()
		b.logger.Warn("relay: rejected unexpected reverse stream from exit", "exit_stream", exitStream.StreamID())
	}
}

// launch wires a single proxy↔exit pair, honoring the active-stream ceiling
// before spawning any work.
func (b *Bridge) launch(parent context.Context, proxySession, exitSession *yamux.Session, proxy, exit *yamux.Stream) {
	b.mu.Lock()
	if b.conns >= maxStreams {
		b.mu.Unlock()
		_ = proxy.Close()
		_ = exit.Close()
		b.logger.Warn("relay: stream limit reached, rejecting proxy stream")
		return
	}
	id := proxy.StreamID()
	b.active[id] = struct{}{}
	b.conns++
	b.mu.Unlock()

	child, cancel := context.WithCancel(parent)

	// Tear the pair down when either session ends or the parent is cancelled,
	// whichever fires first. Watching both sessions' CloseChan keeps a dead
	// proxy connection from parking a watcher goroutine past its useful life.
	go func() {
		select {
		case <-proxySession.CloseChan():
			cancel()
		case <-exitSession.CloseChan():
			cancel()
		case <-parent.Done():
			cancel()
		}
	}()

	go func() {
		defer b.track(id, cancel)
		// The adapter supplies the addresses yamux streams omit so Duplex has
		// real net.Conns to bridge. Finish is the half-close (FIN) path; the
		// forced-close path is handled by Duplex when a direction stalls.
		proxyConn := stream.NewAdapter(proxy,
			stream.NewStreamAddr("tcp", "proxy"),
			stream.NewStreamAddr("tcp", "exit"))
		exitConn := stream.NewAdapter(exit,
			stream.NewStreamAddr("tcp", "exit"),
			stream.NewStreamAddr("tcp", "proxy"))
		dup := stream.NewDuplex(proxyConn, exitConn)
		if err := dup.Run(child); err != nil && !errors.Is(err, context.Canceled) {
			b.logger.Error("relay: bridge copy failed", "error", err)
		}
	}()
}

// track removes a pair from the active set and decrements the counter once its
// copy finishes, releasing the slot for the next connection.
func (b *Bridge) track(id uint32, cancel context.CancelFunc) {
	defer cancel()

	b.mu.Lock()
	delete(b.active, id)
	b.conns--
	b.mu.Unlock()
}

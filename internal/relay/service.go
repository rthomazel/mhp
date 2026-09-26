package relay

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"

	"github.com/hashicorp/yamux"

	"github.com/rthomazel/mhp/internal/config"
	"github.com/rthomazel/mhp/internal/transport"
)

// Service is the relay role. It accepts TLS clients, authenticates each through
// the transport handshake, registers the resulting yamux session in the
// registry under its role, and launches the bridge serve loop that role
// implies: ServeProxy for a proxy client, ServeExit for an exit client.
//
// The registry owns bridge pairing; the Service owns session lifecycle. It is
// deliberately thin: every accepted session is a short-lived worker that
// registers, serves, and vacates, so a crashed or replaced peer cannot park
// goroutines past its useful life.
type Service struct {
	handshake *transport.Handshake
	reg       *Registry
	bridge    *Bridge
	logger    *slog.Logger

	// mu guards sessions, the set of currently-registered sessions. It is
	// only written by the accept goroutine (register) and by Run's shutdown
	// path (closeAll), so holding it across closeAll keeps shutdown race-free.
	mu       sync.Mutex
	sessions map[*transport.Session]struct{}
}

// NewService wires the handshake, registry, and bridge into a relay Service. The
// bridge shares the Service's registry so a replaced registration is seen
// consistently by both the bridge and the ServeExit/ServeProxy loops.
func NewService(handshake *transport.Handshake, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	reg := NewRegistry()
	return &Service{
		handshake: handshake,
		reg:       reg,
		bridge:    NewBridge(reg, logger),
		logger:    logger,
		sessions:  map[*transport.Session]struct{}{},
	}
}

// Run accepts clients on ln until ctx is cancelled or the listener closes, then
// waits for every session's bridge loop to unwind. It uses a derived service
// context so shutting down cancels every in-flight serve loop at once, and it
// closes every registered session so a serve loop parked on AcceptStream
// returns instead of leaking.
func (s *Service) Run(ctx context.Context, ln net.Listener) error {
	svcCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var serveWG sync.WaitGroup
	accDone := make(chan struct{})
	go func() {
		defer close(accDone)
		for {
			session, err := s.handshake.Run(svcCtx, ln)
			if err != nil || session == nil {
				// Accept loop stopped: listener closed, ctx cancelled, or a
				// handshake failure. Either way no more sessions will arrive.
				if err != nil {
					s.logger.Warn("relay: accept loop ended", "error", err)
				}
				return
			}

			s.register(session)
			serveWG.Add(1)
			go func(session *transport.Session) {
				defer serveWG.Done()
				s.serveSession(svcCtx, session)
			}(session)
		}
	}()

	select {
	case <-ctx.Done():
	case <-accDone:
	}

	// Stop the accept goroutine for good, then wait for it to fully return so
	// no session can be registered after we snapshot the set below.
	_ = ln.Close()
	<-accDone

	// Unblock every parked serve loop by closing its session, then cancel the
	// service context so any serve loop not parked on AcceptStream returns too.
	s.closeAll()
	cancel()
	serveWG.Wait()
	return ctx.Err()
}

// serveSession registers one authenticated session, launches its role-specific
// bridge serve loop, and vacates the slot when the session ends. It returns
// only once the serve loop has finished, keeping serveWG balanced.
func (s *Service) serveSession(ctx context.Context, session *transport.Session) {
	defer func() {
		s.mu.Lock()
		delete(s.sessions, session)
		s.mu.Unlock()
	}()

	// Register under the session's role. Register returns the previous holder,
	// which we close outside the lock; a later replacement is unaffected.
	prev, gen := s.reg.Register(session.Role, session.Mux)
	if prev != nil {
		s.logger.Info("relay: session replaced",
			"role", string(session.Role), "session_id", session.ID)
		_ = prev.Close()
	}
	s.logger.Info("relay: session registered",
		"role", string(session.Role), "session_id", session.ID)
	defer s.reg.Vacate(session.Role, gen)

	var serve func(context.Context, *yamux.Session) error
	switch session.Role {
	case config.ModeProxy:
		serve = s.bridge.ServeProxy
	case config.ModeExit:
		serve = s.bridge.ServeExit
	default:
		// The handshake already rejected unknown roles; nothing to serve.
		return
	}

	if err := serve(ctx, session.Mux); err != nil && !errors.Is(err, context.Canceled) {
		s.logger.Debug("relay: serve loop ended",
			"role", string(session.Role), "error", err)
	}
	// Tear down the session so a serve loop parked on AcceptStream returns and
	// its goroutine can be joined. Close is idempotent, so an already-dead
	// session is harmless here.
	_ = session.Close()
}

// register records a session so shutdown can close it. Callers must hold mu.
func (s *Service) register(session *transport.Session) {
	s.mu.Lock()
	s.sessions[session] = struct{}{}
	s.mu.Unlock()
}

// closeAll closes every registered session. It is only invoked after the accept
// goroutine has fully returned, so no new sessions can appear mid-sweep.
func (s *Service) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for session := range s.sessions {
		_ = session.Close()
	}
}

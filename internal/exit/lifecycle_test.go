package exit

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestSetupDeadlineInterruptsSilentClient(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = client.Close() }()
	h := New(Options{SetupTimeout: 20 * time.Millisecond})
	done := make(chan error, 1)
	go func() { done <- h.ServeConn(context.Background(), server) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected setup timeout")
		}
	case <-time.After(time.Second):
		t.Fatal("silent SOCKS client hung")
	}
}

func TestSessionCancelInterruptsSetup(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = client.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- New(Options{}).ServeConn(ctx, server) }()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancelled setup failure")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled setup hung")
	}
}

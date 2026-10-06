package stream

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/hashicorp/yamux"
)

func tcpPair(t *testing.T) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	server, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	_ = server.SetDeadline(time.Now().Add(3 * time.Second))
	return client.(*net.TCPConn), server.(*net.TCPConn)
}

func TestDuplexPropagatesFINThroughYamux(t *testing.T) {
	browser, local := tcpPair(t)
	target, remote := tcpPair(t)
	a, b := net.Pipe()
	cfg := yamux.DefaultConfig()
	cfg.LogOutput = io.Discard
	left, err := yamux.Client(a, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = left.Close() }()
	right, err := yamux.Server(b, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = right.Close() }()
	ls, err := left.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	rs, err := right.AcceptStream()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 2)
	go func() { done <- NewDuplex(local, ls).Run(ctx) }()
	go func() { done <- NewDuplex(rs, remote).Run(ctx) }()
	if _, err := browser.Write([]byte("request")); err != nil {
		t.Fatal(err)
	}
	_ = browser.CloseWrite()
	request, err := io.ReadAll(target)
	if err != nil || string(request) != "request" {
		t.Fatalf("request=%q err=%v", request, err)
	}
	if _, err := target.Write([]byte("response after EOF")); err != nil {
		t.Fatal(err)
	}
	_ = target.CloseWrite()
	response, err := io.ReadAll(browser)
	if err != nil || string(response) != "response after EOF" {
		t.Fatalf("response=%q err=%v", response, err)
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestDuplexBoundsHalfClosedDrain(t *testing.T) {
	browser, local := tcpPair(t)
	_, remote := tcpPair(t)
	dup := NewDuplex(local, remote)
	dup.drainTimeout = 20 * time.Millisecond
	_ = browser.CloseWrite()
	done := make(chan error, 1)
	go func() { done <- dup.Run(context.Background()) }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrDrainTimeout) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("half-closed bridge hung")
	}
}

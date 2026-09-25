package stream

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// errBoom is the sentinel a failing direction must return.
var errBoom = errors.New("kaboom")

// errorConn wraps a net.Conn, returning readErr from Read and recording Close.
type errorConn struct {
	net.Conn
	readErr error
	closed  bool
}

func (e *errorConn) Read(b []byte) (int, error) {
	if e.readErr != nil {
		return 0, e.readErr
	}
	return e.Conn.Read(b)
}

func (e *errorConn) Close() error {
	e.closed = true
	return e.Conn.Close()
}

// readExactly reads exactly len(want) bytes and returns them.
func readExactly(t *testing.T, c net.Conn, want string) []byte {
	t.Helper()
	buf := make([]byte, len(want))
	n, err := io.ReadFull(c, buf)
	if err != nil {
		t.Fatalf("io.ReadFull(%q): n=%d, err=%v", want, n, err)
	}
	return buf[:n]
}

// TestDuplexCopiesBothDirections verifies each direction relays bytes from the
// outer write end to the opposite outer read end. Each direction is exercised
// on its own Duplex pair so the read end sees only the bytes that direction
// produces; Duplex relays everything bidirectionally, so mixing the writes on
// a single pair would interleave the streams.
func TestDuplexCopiesBothDirections(t *testing.T) {
	t.Run("outer to inner", func(t *testing.T) {
		writeSide, dupA := net.Pipe()
		dupB, readSide := net.Pipe()
		defer dupA.Close()
		defer dupB.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = NewDuplex(dupA, dupB).Run(ctx) }()

		go func() { _, _ = writeSide.Write([]byte("hello-out")) }()

		got := readExactly(t, readSide, "hello-out")
		if string(got) != "hello-out" {
			t.Fatalf("want %q, got %q", "hello-out", got)
		}
	})

	t.Run("inner to outer", func(t *testing.T) {
		writeSide, dupA := net.Pipe()
		dupB, readSide := net.Pipe()
		defer dupA.Close()
		defer dupB.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = NewDuplex(dupA, dupB).Run(ctx) }()

		go func() { _, _ = writeSide.Write([]byte("hello-in")) }()

		got := readExactly(t, readSide, "hello-in")
		if string(got) != "hello-in" {
			t.Fatalf("want %q, got %q", "hello-in", got)
		}
	})
}

// TestDuplexCancelForcesClose verifies a cancelled context unblocks a stalled
// copy and Run returns the context error.
func TestDuplexCancelForcesClose(t *testing.T) {
	inInner, inOuter := net.Pipe()
	outInner, outOuter := net.Pipe()
	defer inInner.Close()
	defer outInner.Close()
	defer inOuter.Close()
	defer outOuter.Close()

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- NewDuplex(inInner, outInner).Run(ctx) }()

	// Keep both copy directions busy so cancellation is what ends Run.
	go func() {
		for {
			if _, err := inOuter.Write([]byte("x")); err != nil {
				return
			}
		}
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-runErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

// TestDuplexForcesCloseOnFatalError verifies that a real read error on one side
// makes Run return that error and force-closes the opposite side.
func TestDuplexForcesCloseOnFatalError(t *testing.T) {
	realIn, _ := net.Pipe()
	realOut, _ := net.Pipe()

	in := &errorConn{Conn: realIn, readErr: errBoom}
	out := &errorConn{Conn: realOut}

	runErr := make(chan error, 1)
	go func() { runErr <- NewDuplex(in, out).Run(context.Background()) }()

	select {
	case err := <-runErr:
		if !errors.Is(err, errBoom) {
			t.Fatalf("want errBoom, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after fatal error")
	}

	// The forced close must have torn down the opposite side too.
	if !out.closed {
		t.Fatal("fatal error on one side did not force-close the opposite side")
	}
}

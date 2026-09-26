package exit

import (
	"context"
	"net"
	"testing"
	"time"

	statute "github.com/things-go/go-socks5/statute"
)

// tcpFakeDialer is a Dialer whose targets are real TCP listeners, so the
// destination socket's LocalAddr is a genuine *net.TCPAddr (the SOCKS5 reply
// encoder only trusts that concrete type). It records the address it was asked
// to dial so tests can assert the exit dialed the validated IP.
type tcpFakeDialer struct {
	dialed []string
}

func (d *tcpFakeDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	d.dialed = append(d.dialed, addr)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	go func() {
		srv, err := ln.Accept()
		if err != nil {
			return
		}
		// Echo back whatever the client sends, then half-close so the
		// client's copy sees EOF and the exchange completes cleanly.
		buf := make([]byte, 256)
		for {
			n, err := srv.Read(buf)
			if n > 0 {
				srv.Write(buf[:n])
			}
			if err != nil {
				_ = srv.Close()
				_ = ln.Close()
				return
			}
		}
	}()
	cl, _ := net.Dial("tcp", ln.Addr().String())
	return cl, nil
}

// socksClient is a minimal SOCKS5 client: method negotiation, a CONNECT
// request, and reply parsing.
type socksClient struct{ conn net.Conn }

func (c *socksClient) negotiate(method byte) error {
	_, err := c.conn.Write([]byte{0x05, 0x01, method})
	return err
}

func (c *socksClient) readMethodReply() (byte, error) {
	var b [2]byte
	if _, err := readFull(c.conn, b[:]); err != nil {
		return 0, err
	}
	return b[1], nil
}

func (c *socksClient) writeCommand(command byte, atyp byte, host []byte, port uint16) error {
	cmd := []byte{0x05, command, 0x00, atyp}
	cmd = append(cmd, host...)
	cmd = append(cmd, byte(port>>8), byte(port))
	_, err := c.conn.Write(cmd)
	return err
}

func (c *socksClient) connectRequest(atyp byte, host []byte, port uint16) error {
	return c.writeCommand(statute.CommandConnect, atyp, host, port)
}

func (c *socksClient) readConnectReply() (rep byte, err error) {
	// A full SOCKS reply is 10 bytes: VER REP RSV ATYP IP(4) PORT(2).
	var b [10]byte
	if _, err = readFull(c.conn, b[:]); err != nil {
		return 0, err
	}
	return b[1], nil
}

func (c *socksClient) setDeadline(after time.Duration) {
	_ = c.conn.SetDeadline(time.Now().Add(after))
}

func readFull(r net.Conn, b []byte) (int, error) {
	n := 0
	for n < len(b) {
		m, err := r.Read(b[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// serveHandler starts the Handler on a real TCP listener and returns the client
// connection plus a channel carrying ServeConn's completion.
func serveHandler(t *testing.T, h *Handler) (client net.Conn, done chan error, addr string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	done = make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		done <- h.ServeConn(context.Background(), conn)
	}()
	client, err = net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return client, done, ln.Addr().String()
}

// TestHandlerAccessDenied verifies a loopback destination is refused with
// RepRuleFailure and that no dial occurs.
func TestHandlerAccessDenied(t *testing.T) {
	rec := &tcpFakeDialer{}
	h := New(Options{Logger: nil, Dialer: rec})

	client, done, _ := serveHandler(t, h)
	t.Cleanup(func() { client.Close() })
	cli := &socksClient{conn: client}

	if err := cli.negotiate(0x00); err != nil {
		t.Fatalf("negotiate: %v", err)
	}
	if rep, err := cli.readMethodReply(); err != nil || rep != 0x00 {
		t.Fatalf("method reply: rep=%d err=%v", rep, err)
	}

	host := net.ParseIP("127.0.0.1").To4()
	if err := cli.connectRequest(statute.ATYPIPv4, host, 80); err != nil {
		t.Fatalf("connect request: %v", err)
	}
	cli.setDeadline(2 * time.Second)
	rep, err := cli.readConnectReply()
	if err != nil {
		t.Fatalf("connect reply: %v", err)
	}
	if rep != statute.RepRuleFailure {
		t.Fatalf("expected RepRuleFailure(%d), got %d", statute.RepRuleFailure, rep)
	}
	if len(rec.dialed) != 0 {
		t.Fatalf("dialer must not be called, dialed %v", rec.dialed)
	}
	<-done
}

// TestHandlerNonConnectRejected verifies BIND is refused (rule rejects before
// the command switch) and no dial occurs.
func TestHandlerNonConnectRejected(t *testing.T) {
	rec := &tcpFakeDialer{}
	h := New(Options{Logger: nil, Dialer: rec})

	client, done, _ := serveHandler(t, h)
	t.Cleanup(func() { client.Close() })
	cli := &socksClient{conn: client}

	if err := cli.negotiate(0x00); err != nil {
		t.Fatalf("negotiate: %v", err)
	}
	if rep, err := cli.readMethodReply(); err != nil || rep != 0x00 {
		t.Fatalf("method reply: rep=%d err=%v", rep, err)
	}

	host := net.ParseIP("1.2.3.4").To4()
	// BIND command, IPv4 ATYP.
	if err := cli.writeCommand(statute.CommandBind, statute.ATYPIPv4, host, 9); err != nil {
		t.Fatalf("write command: %v", err)
	}
	cli.setDeadline(2 * time.Second)
	rep, err := cli.readConnectReply()
	if err != nil {
		t.Fatalf("connect reply: %v", err)
	}
	if rep != statute.RepRuleFailure {
		t.Fatalf("expected RepRuleFailure(%d), got %d", statute.RepRuleFailure, rep)
	}
	if len(rec.dialed) != 0 {
		t.Fatalf("dialer must not be called, dialed %v", rec.dialed)
	}
	<-done
}

// TestHandlerSuccessfulDial verifies a public destination dials through the
// fake dialer, reports RepSuccess, and proxies a payload bidirectionally.
func TestHandlerSuccessfulDial(t *testing.T) {
	rec := &tcpFakeDialer{}
	h := New(Options{Logger: nil, Dialer: rec})

	client, done, _ := serveHandler(t, h)
	t.Cleanup(func() { client.Close() })
	cli := &socksClient{conn: client}

	if err := cli.negotiate(0x00); err != nil {
		t.Fatalf("negotiate: %v", err)
	}
	if rep, err := cli.readMethodReply(); err != nil || rep != 0x00 {
		t.Fatalf("method reply: rep=%d err=%v", rep, err)
	}

	host := net.ParseIP("93.184.216.34").To4()
	if err := cli.connectRequest(statute.ATYPIPv4, host, 8080); err != nil {
		t.Fatalf("connect request: %v", err)
	}
	cli.setDeadline(2 * time.Second)
	rep, err := cli.readConnectReply()
	if err != nil {
		t.Fatalf("connect reply: %v", err)
	}
	if rep != statute.RepSuccess {
		t.Fatalf("expected RepSuccess(%d), got %d", statute.RepSuccess, rep)
	}
	if len(rec.dialed) != 1 || rec.dialed[0] != "93.184.216.34:8080" {
		t.Fatalf("expected single dial to 93.184.216.34:8080, got %v", rec.dialed)
	}

	// The fake dialer echoes; send a payload and read it back.
	payload := []byte("hello-from-exit")
	if _, err := cli.conn.Write(payload); err != nil {
		t.Fatalf("client write: %v", err)
	}
	buf := make([]byte, len(payload))
	if _, err := readFull(cli.conn, buf); err != nil {
		t.Fatalf("client read payload: %v", err)
	}
	if string(buf) != string(payload) {
		t.Fatalf("payload mismatch: %q", buf)
	}
	// Drive the destination side to EOF so both copy loops finish and
	// ServeConn returns, letting us assert clean shutdown.
	_ = cli.conn.(*net.TCPConn).CloseWrite()
	<-done
}

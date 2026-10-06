// Package stream provides cancellation-aware bidirectional connection copying.
package stream

import (
	"context"
	"errors"
	"io"
	"net"
	"time"

	"github.com/hashicorp/yamux"
)

// ErrDrainTimeout means the reverse direction did not finish after EOF.
var ErrDrainTimeout = errors.New("stream: half-close drain timeout")

// Duplex owns both connections until Run returns.
type Duplex struct {
	inbound      net.Conn
	outbound     net.Conn
	drainTimeout time.Duration
}

func NewDuplex(inbound, outbound net.Conn) *Duplex {
	return &Duplex{inbound: inbound, outbound: outbound, drainTimeout: 30 * time.Second}
}

// Run propagates FIN on EOF, allowing the reverse direction a bounded drain.
// Cancellation and copy failures interrupt both directions; all workers are joined.
func (d *Duplex) Run(ctx context.Context) error {
	results := make(chan error, 2)
	copier := func(from, to net.Conn) {
		_, err := io.Copy(to, from)
		if err == nil {
			err = CloseWrite(to)
		}
		results <- err
	}
	go copier(d.inbound, d.outbound)
	go copier(d.outbound, d.inbound)
	remaining := 2
	defer func() {
		d.forceClose()
		for remaining > 0 {
			<-results
			remaining--
		}
	}()
	var timer *time.Timer
	var timeout <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for remaining > 0 {
		select {
		case err := <-results:
			remaining--
			if err != nil {
				return err
			}
			if remaining > 0 {
				timer = time.NewTimer(d.drainTimeout)
				timeout = timer.C
			}
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return ErrDrainTimeout
		}
	}
	return nil
}

// CloseWrite propagates FIN without cutting off the reverse response.
// In the pinned yamux version Close is a write-side FIN, retaining reads.
func CloseWrite(conn net.Conn) error {
	if cw, ok := conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	if ys, ok := conn.(*yamux.Stream); ok {
		return ys.Close()
	}
	return nil
}

func (d *Duplex) forceClose() {
	deadline := time.Now().Add(-time.Second)
	_ = d.inbound.SetDeadline(deadline)
	_ = d.outbound.SetDeadline(deadline)
	_ = d.inbound.Close()
	_ = d.outbound.Close()
}

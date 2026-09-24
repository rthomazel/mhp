package transport

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/rthomazel/mhp/internal/config"
)

// helloVersion pins the single protocol version accepted by both peers.
const helloVersion uint16 = 1

// ErrProtocolVersion is returned when a record advertises a version other than helloVersion.
var ErrProtocolVersion = errors.New("transport: unexpected protocol version")

// ErrUnexpectedRole is returned when Hello.Role is neither exit nor proxy.
var ErrUnexpectedRole = errors.New("transport: unexpected role")

// ErrUnexpectedStatus is returned when Response.Status is neither ok nor error.
var ErrUnexpectedStatus = errors.New("transport: unexpected status")

// Mode aliases config.Mode so transport can speak roles without spelling the
// package path on every signature.
type Mode = config.Mode

// Hello is the client's hello to the relay. Sent after the TLS handshake, before
// yamux is started.
type Hello struct {
	Version uint16 `json:"version"`
	Role    Mode   `json:"role"`
	Token   string `json:"token"`
}

// Response is the relay's verdict. Sent only after the hello.
type Response struct {
	Version   uint16 `json:"version"`
	Status    Status `json:"status"`
	SessionID string `json:"session_id,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

// Status is the relay's verdict: ok or error.
type Status string

const (
	statusOK    Status = "ok"
	statusError Status = "error"
)

// validateHello checks the client's hello before the relay acts on it.
func validateHello(hello Hello) error {
	if hello.Version != helloVersion {
		return fmt.Errorf("%w: %d", ErrProtocolVersion, hello.Version)
	}
	switch hello.Role {
	case config.ModeExit, config.ModeProxy:
	default:
		return fmt.Errorf("%w: %q", ErrUnexpectedRole, hello.Role)
	}
	return nil
}

// validateResponse checks the relay's verdict before the client acts on it.
func validateResponse(resp Response) error {
	if resp.Version != helloVersion {
		return fmt.Errorf("%w: %d", ErrProtocolVersion, resp.Version)
	}
	switch resp.Status {
	case statusOK, statusError:
	default:
		return fmt.Errorf("%w: %q", ErrUnexpectedStatus, resp.Status)
	}
	return nil
}

// newSessionID derives a random 16-byte hex handle issued by the relay.
func newSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("transport: read random: %v", err))
	}
	return hex.EncodeToString(b[:])
}

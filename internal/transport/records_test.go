package transport

import (
	"errors"
	"testing"

	"github.com/rthomazel/mhp/internal/config"
)

func TestValidateHello(t *testing.T) {
	good := Hello{Version: helloVersion, Role: config.ModeExit, Token: "tok"}
	if err := validateHello(good); err != nil {
		t.Fatalf("want nil, got %v", err)
	}

	if err := validateHello(Hello{Version: 99, Role: config.ModeExit}); !errors.Is(err, ErrProtocolVersion) {
		t.Fatalf("want ErrProtocolVersion, got %v", err)
	}
	if err := validateHello(Hello{Version: helloVersion, Role: "ghost"}); !errors.Is(err, ErrUnexpectedRole) {
		t.Fatalf("want ErrUnexpectedRole, got %v", err)
	}
}

func TestValidateResponse(t *testing.T) {
	good := Response{Version: helloVersion, Status: statusOK, SessionID: "s1"}
	if err := validateResponse(good); err != nil {
		t.Fatalf("want nil, got %v", err)
	}

	if err := validateResponse(Response{Version: 7, Status: statusOK}); !errors.Is(err, ErrProtocolVersion) {
		t.Fatalf("want ErrProtocolVersion, got %v", err)
	}
	if err := validateResponse(Response{Version: helloVersion, Status: "weird"}); !errors.Is(err, ErrUnexpectedStatus) {
		t.Fatalf("want ErrUnexpectedStatus, got %v", err)
	}
}

func TestNewSessionID(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 1000; i++ {
		id := newSessionID()
		if len(id) != 32 { // 16 bytes hex-encoded
			t.Fatalf("id length %d, want 32", len(id))
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate session id %q", id)
		}
		seen[id] = struct{}{}
	}
}

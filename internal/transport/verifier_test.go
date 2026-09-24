package transport

import (
	"errors"
	"testing"

	"github.com/rthomazel/mhp/internal/config"
)

func TestVerifierVerify(t *testing.T) {
	v := Verifier{ExpectedExits: map[Mode]string{config.ModeProxy: "proxy-token"}}

	if err := v.verify(config.ModeProxy, "proxy-token"); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if err := v.verify(config.ModeProxy, ""); !errors.Is(err, ErrEmptyBearer) {
		t.Fatalf("want ErrEmptyBearer, got %v", err)
	}
	if err := v.verify(config.ModeProxy, "wrong"); !errors.Is(err, ErrWrongToken) {
		t.Fatalf("want ErrWrongToken, got %v", err)
	}
	if err := v.verify(config.ModeExit, "whatever"); !errors.Is(err, ErrBadRole) {
		t.Fatalf("want ErrBadRole, got %v", err)
	}
}

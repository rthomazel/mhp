package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("secret-token"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func asValidationError(err error) (*ValidationError, bool) {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return ve, true
	}
	return nil, false
}

func TestValidModes(t *testing.T) {
	exitTok := writeFile(t, "exit.token")
	proxyTok := writeFile(t, "proxy.token")

	tests := []struct {
		name    string
		in      ParsedFlags
		wantErr bool
	}{
		{
			name: "relay complete",
			in: ParsedFlags{
				Mode:           ModeRelay,
				Listen:         ":443",
				TLSCert:        "relay.crt",
				TLSKey:         "relay.key",
				ExitTokenFile:  exitTok,
				ProxyTokenFile: proxyTok,
			},
		},
		{
			name: "exit complete",
			in: ParsedFlags{
				Mode:      ModeExit,
				Relay:     "relay.example.com:443",
				TLSName:   "en.zalando.de",
				CAPath:    "ca.pem",
				TokenFile: exitTok,
			},
		},
		{
			name: "proxy complete",
			in: ParsedFlags{
				Mode:      ModeProxy,
				Listen:    "127.0.0.1:1080",
				Relay:     "relay.example.com:443",
				TLSName:   "en.zalando.de",
				CAPath:    "ca.pem",
				TokenFile: proxyTok,
			},
		},
		{
			name:    "invalid mode",
			in:      ParsedFlags{Mode: "bogus"},
			wantErr: true,
		},
		{
			name:    "relay missing cert",
			in:      ParsedFlags{Mode: ModeRelay, Listen: ":443", ExitTokenFile: exitTok, ProxyTokenFile: proxyTok},
			wantErr: true,
		},
		{
			name:    "relay missing token files",
			in:      ParsedFlags{Mode: ModeRelay, Listen: ":443", TLSCert: "c", TLSKey: "k"},
			wantErr: true,
		},
		{
			name:    "exit missing relay",
			in:      ParsedFlags{Mode: ModeExit, TokenFile: exitTok},
			wantErr: true,
		},
		{
			name:    "proxy missing token file",
			in:      ParsedFlags{Mode: ModeProxy, Listen: "127.0.0.1:1080", Relay: "x:443"},
			wantErr: true,
		},
		{
			name:    "relay missing token file",
			in:      ParsedFlags{Mode: ModeRelay, Listen: ":443", TLSCert: "c", TLSKey: "k", ExitTokenFile: exitTok},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(tc.in)
			if tc.wantErr != (err != nil) {
				t.Fatalf("wantErr=%v, got err=%v", tc.wantErr, err)
			}
		})
	}
}

func TestInvalidModeRejectedByLoad(t *testing.T) {
	// parseFlags only validates flag definitions; semantic validity (a known
	// mode) is config.Load's responsibility.
	_, err := Load(ParsedFlags{Mode: "bogus"})
	if err == nil {
		t.Fatal("expected error for unrecognized mode")
	}
	if _, ok := asValidationError(err); !ok {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
}

func TestProxyListenNonLoopbackRejected(t *testing.T) {
	proxyTok := writeFile(t, "proxy.token")
	_, err := Load(ParsedFlags{
		Mode:      ModeProxy,
		Listen:    "0.0.0.0:1080",
		Relay:     "relay.example.com:443",
		CAPath:    "ca.pem",
		TokenFile: proxyTok,
	})
	if err == nil {
		t.Fatal("expected non-loopback proxy listen to be rejected")
	}
	ve, ok := asValidationError(err)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	if ve.Field != "-listen" {
		t.Fatalf("expected field -listen, got %q", ve.Field)
	}
	if ve.Reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestLoopbackAccepts(t *testing.T) {
	if !isLoopback("127.0.0.1:1080") {
		t.Error("127.0.0.1 should be loopback")
	}
	if !isLoopback("[::1]:5000") {
		t.Error("::1 should be loopback")
	}
	if isLoopback("0.0.0.0:1080") {
		t.Error("0.0.0.0 should NOT be loopback")
	}
	if isLoopback("8.8.8.8:443") {
		t.Error("public IP should NOT be loopback")
	}
	if isLoopback("192.168.1.1:1080") {
		t.Error("private IP should NOT be loopback")
	}
}

func TestSecretEquality(t *testing.T) {
	a := Secret{bytes: []byte("abc")}
	b := Secret{bytes: []byte("abc")}
	c := Secret{bytes: []byte("abd")}
	if !a.Equal(b) {
		t.Error("equal secrets compare unequal")
	}
	if a.Equal(c) {
		t.Error("different secrets compare equal")
	}
}

func TestSecretDoesNotLeak(t *testing.T) {
	// Secret must not satisfy the Stringer or Formatter interfaces, so it can
	// never leak its bytes through %v, %s, or fmt formatting.
	var s = Secret{bytes: []byte("supersecret")}
	if _, ok := any(&s).(fmt.Stringer); ok {
		t.Error("Secret must not be a Stringer")
	}
	if _, ok := any(&s).(fmt.Formatter); ok {
		t.Error("Secret must not be a Formatter")
	}
	// Value() returns a copy, so mutating it must not alter the secret.
	buf := s.Value()
	buf[0] = 'X'
	if string(s.Value()) != "supersecret" {
		t.Error("mutating the copy leaked into the secret")
	}
}

func TestLoadBadTokenFile(t *testing.T) {
	_, err := Load(ParsedFlags{
		Mode:      ModeExit,
		Relay:     "relay.example.com:443",
		CAPath:    "ca.pem",
		TokenFile: filepath.Join(t.TempDir(), "does-not-exist.token"),
	})
	if err == nil {
		t.Fatal("expected error for missing token file")
	}
}

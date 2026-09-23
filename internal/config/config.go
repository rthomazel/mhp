// Package config builds and validates the configuration object from parsed
// flags. It enforces mode-specific required options, rejects unsafe proxy
// listen addresses, and loads bearer-token files without surfacing their
// contents anywhere.
//
// Validation and secret loading happen together in Load so a caller receives
// one validated Config or an error. No comparison of secrets occurs here;
// Load only reads files and stores opaque token values. The logging layer is
// responsible for never serializing those values.
package config

import (
	"bytes"
	"crypto/subtle"
	"fmt"
	"net"
	"os"
)

// Mode is a server role.
type Mode string

const (
	// ModeRelay listens for exit and proxy clients on a public address.
	ModeRelay Mode = "relay"
	// ModeExit connects to the relay as a client and serves SOCKS5 to the
	// browser.
	ModeExit Mode = "exit"
	// ModeProxy connects to the relay as a client and serves a local SOCKS5
	// proxy.
	ModeProxy Mode = "proxy"
)

// Config is the validated configuration for a run.
type Config struct {
	Mode Mode

	// Listen is the relay listen address (e.g. :443) or the proxy listen
	// address (loopback). Blank for exit.
	Listen string

	// Relay is the relay address (host:port) the exit/proxy connect to. Blank
	// for relay.
	Relay string

	// TLSName is the SNI/server-name override the client presents.
	TLSName string

	// CAPath is the path to the trusted CA certificate file.
	CAPath string

	// TLSCert is the path to the relay leaf certificate. Only relay.
	TLSCert string

	// TLSKey is the path to the relay private key. Only relay.
	TLSKey string

	// TokenFile is the path to the single-role token file. Only exit/proxy.
	TokenFile string

	// ExitTokenFile is the path to relay's exit token file. Only relay.
	ExitTokenFile string

	// ProxyTokenFile is the path to relay's proxy token file. Only relay.
	ProxyTokenFile string

	// ExitToken is the loaded relay->exit bearer. Only relay.
	ExitToken Secret

	// ProxyToken is the loaded relay->proxy bearer. Only relay.
	ProxyToken Secret

	// Token is the loaded presented-role bearer. Only exit/proxy.
	Token Secret
}

// Mode returns the mode string.
func (m Mode) String() string { return string(m) }

// Secret is an opaque token value. It holds raw bytes and never formats them.
type Secret struct {
	bytes []byte
}

// Value returns a copy of the token bytes without exposing them to
// serialization.
func (s Secret) Value() []byte { return append([]byte(nil), s.bytes...) }

// Equal reports whether two secrets are equal using constant-time comparison.
func (s Secret) Equal(o Secret) bool {
	return subtle.ConstantTimeCompare(s.bytes, o.bytes) == 1
}

// ValidationError describes which option failed and why.
type ValidationError struct {
	Field  string
	Reason string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Reason)
}

// ParsedFlags is the input to Load: the parsed flag values. Keeping it a plain
// struct (rather than pulling in the flag package) lets tests construct configs
// directly and decouples validation from parsing.
type ParsedFlags struct {
	Mode           Mode
	Listen         string
	Relay          string
	TLSName        string
	CAPath         string
	TLSCert        string
	TLSKey         string
	TokenFile      string
	ExitTokenFile  string
	ProxyTokenFile string
	Debug          bool
}

// Load builds and validates the configuration from parsed flags.
//
// It determines the mode, checks required flags, guards the proxy listen
// address, and loads the role-appropriate token file(s). It returns one
// validated Config or a ValidationError.
func Load(parsed ParsedFlags) (Config, error) {
	cfg, err := build(parsed)
	if err != nil {
		return Config{}, err
	}
	if verr := validate(&cfg); verr != nil {
		return Config{}, verr
	}
	if err := loadTokens(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// build constructs a Config skeleton from parsed flag values.
func build(parsed ParsedFlags) (Config, error) {
	switch parsed.Mode {
	case ModeRelay:
		return Config{
			Mode:           ModeRelay,
			Listen:         parsed.Listen,
			TLSCert:        parsed.TLSCert,
			TLSKey:         parsed.TLSKey,
			ExitTokenFile:  parsed.ExitTokenFile,
			ProxyTokenFile: parsed.ProxyTokenFile,
		}, nil
	case ModeExit:
		return Config{
			Mode:      ModeExit,
			Relay:     parsed.Relay,
			TLSName:   parsed.TLSName,
			CAPath:    parsed.CAPath,
			TokenFile: parsed.TokenFile,
		}, nil
	case ModeProxy:
		return Config{
			Mode:      ModeProxy,
			Listen:    parsed.Listen,
			Relay:     parsed.Relay,
			TLSName:   parsed.TLSName,
			CAPath:    parsed.CAPath,
			TokenFile: parsed.TokenFile,
		}, nil
	default:
		return Config{}, &ValidationError{Field: "mode", Reason: "unrecognized mode"}
	}
}

// validate enforces mode-specific required flags and the proxy listen guard.
func validate(cfg *Config) error {
	switch cfg.Mode {
	case ModeRelay:
		for f, v := range map[string]string{
			"-listen":           cfg.Listen,
			"-tls-cert":         cfg.TLSCert,
			"-tls-key":          cfg.TLSKey,
			"-exit-token-file":  cfg.ExitTokenFile,
			"-proxy-token-file": cfg.ProxyTokenFile,
		} {
			if v == "" {
				return &ValidationError{Field: f, Reason: "required for relay"}
			}
		}
	case ModeExit, ModeProxy:
		if cfg.Relay == "" {
			return &ValidationError{Field: "-relay", Reason: "required for " + string(cfg.Mode)}
		}
		if cfg.TokenFile == "" {
			return &ValidationError{Field: "-token-file", Reason: "required for " + string(cfg.Mode)}
		}
	}
	if cfg.Mode == ModeProxy && !isLoopback(cfg.Listen) {
		return &ValidationError{Field: "-listen", Reason: "proxy listen must be loopback"}
	}
	return nil
}

// loadTokens loads the role-appropriate token file(s).
func loadTokens(cfg *Config) error {
	switch cfg.Mode {
	case ModeRelay:
		exit, err := loadToken(cfg.ExitTokenFile)
		if err != nil {
			return err
		}
		proxy, err := loadToken(cfg.ProxyTokenFile)
		if err != nil {
			return err
		}
		cfg.ExitToken = exit
		cfg.ProxyToken = proxy
		return nil
	case ModeExit, ModeProxy:
		token, err := loadToken(cfg.TokenFile)
		if err != nil {
			return err
		}
		cfg.Token = token
		return nil
	default:
		return &ValidationError{Field: "mode", Reason: "cannot load tokens"}
	}
}

// isLoopback reports whether addr (possibly host:port) is a loopback address.
// Only strict loopback is acceptable: 127.0.0.0/8 and ::1.
func isLoopback(addr string) bool {
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// loadToken reads a token file and returns a trimmed Secret.
func loadToken(path string) (Secret, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Secret{}, fmt.Errorf("read token file %q: %w", path, err)
	}
	return Secret{bytes: bytes.TrimSpace(data)}, nil
}

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rthomazel/mhp/internal/config"
)

func writeFile(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("secret-token"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	return path
}

func TestParseFlagsExtraction(t *testing.T) {
	argv := []string{
		"-mode", "relay",
		"-listen", ":443",
		"-tls-cert", "relay.crt",
		"-tls-key", "relay.key",
		"-exit-token-file", "exit.token",
		"-proxy-token-file", "proxy.token",
		"-debug",
	}
	flags, err := parseFlags(argv)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if flags.Mode != "relay" {
		t.Errorf("mode = %q, want relay", flags.Mode)
	}
	if flags.Listen != ":443" {
		t.Errorf("listen = %q, want :443", flags.Listen)
	}
	if !flags.Debug {
		t.Error("debug flag not parsed")
	}
	if flags.ExitTokenFile != "exit.token" {
		t.Errorf("exit token file = %q", flags.ExitTokenFile)
	}
	if flags.ProxyTokenFile != "proxy.token" {
		t.Errorf("proxy token file = %q", flags.ProxyTokenFile)
	}
}

func TestParseFlagsExitMode(t *testing.T) {
	argv := []string{
		"-mode", "exit",
		"-relay", "relay.example.com:443",
		"-tls-name", "en.zalando.de",
		"-ca", "ca.pem",
		"-token-file", "exit.token",
	}
	flags, err := parseFlags(argv)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if flags.Mode != "exit" {
		t.Errorf("mode = %q, want exit", flags.Mode)
	}
	if flags.Relay != "relay.example.com:443" {
		t.Errorf("relay = %q", flags.Relay)
	}
	if flags.TLSName != "en.zalando.de" {
		t.Errorf("tls-name = %q", flags.TLSName)
	}
	if flags.CAPath != "ca.pem" {
		t.Errorf("ca = %q", flags.CAPath)
	}
}

func TestParseFlagsUnknownFlag(t *testing.T) {
	if _, err := parseFlags([]string{"-mode", "exit", "-relay", "r:443", "-cafe", "nope"}); err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestRunWithContextInvalidConfigReturnsProvisionError(t *testing.T) {
	// Incomplete relay config must yield a provision-level error, not a panic.
	err := runWithContext(context.Background(), config.ParsedFlags{Mode: "relay"})
	if err == nil {
		t.Fatal("expected error for incomplete relay config")
	}
}

func TestRunWithContextGracefulShutdown(t *testing.T) {
	token := writeFile(t, "exit.token")
	flags := config.ParsedFlags{
		Mode:      "exit",
		Relay:     "127.0.0.1:443",
		TLSName:   "en.zalando.de",
		CAPath:    "ca.pem",
		TokenFile: token,
	}

	// Pre-cancelled context simulates Ctrl+C arriving before any work happens.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err := runWithContext(ctx, flags)
	if err != nil {
		t.Errorf("graceful shutdown returned error: %v", err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Errorf("shutdown took too long: %v", time.Since(start))
	}
}

func TestRunWithContextCancelledMidRun(t *testing.T) {
	token := writeFile(t, "exit.token")
	flags := config.ParsedFlags{
		Mode:      "exit",
		Relay:     "127.0.0.1:443",
		TLSName:   "en.zalando.de",
		CAPath:    "ca.pem",
		TokenFile: token,
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := runWithContext(ctx, flags)
	if err != nil {
		t.Errorf("cancel mid-run returned error: %v", err)
	}
}

func TestExitCodeContract(t *testing.T) {
	if exitProvision != 2 {
		t.Errorf("exitProvision = %d, want 2", exitProvision)
	}
	if exitOK != 0 {
		t.Errorf("exitOK = %d, want 0", exitOK)
	}
}

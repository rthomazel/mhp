package logging

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNewSetupConsoleOnly(t *testing.T) {
	l, err := NewSetup("exit-node", false, "")
	if err != nil {
		t.Fatalf("NewSetup: %v", err)
	}
	defer func() { _ = l.Closer.Close() }()
	if l.Slog == nil {
		t.Fatal("expected non-nil Slog")
	}
	// Even without a debug file, the closer must be present and safe to call
	// so callers never special-case its absence.
	if l.Closer == nil {
		t.Error("expected a usable Closer even in console-only mode")
	}
	if err := l.Closer.Close(); err != nil {
		t.Errorf("closing the console-only closer failed: %v", err)
	}
}

func TestNewSetupCreatesDebugFile(t *testing.T) {
	dir := t.TempDir()
	l, err := NewSetup("relay", true, dir)
	if err != nil {
		t.Fatalf("NewSetup: %v", err)
	}
	defer func() { _ = l.Closer.Close() }()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "mhp-debug-relay-") {
			found = true
		}
	}
	if !found {
		t.Error("expected mhp-debug-relay-*.txt file to be created")
	}
}

func TestDebugFileCollisionSafeNaming(t *testing.T) {
	dir := t.TempDir()
	// Force a collision: pre-create the exact base name the next openDebugFile
	// call will attempt, proving the collision path yields a distinct file
	// rather than truncating the existing one.
	baseName := fmt.Sprintf(debugLogPattern, "exit-node", time.Now().Unix())
	preExisting := filepath.Join(dir, baseName)
	if err := os.WriteFile(preExisting, []byte("victim"), 0o600); err != nil {
		t.Fatalf("seed collision file: %v", err)
	}

	_, file, err := openDebugFile("exit-node", dir)
	if err != nil {
		t.Fatalf("openDebugFile: %v", err)
	}
	defer func() { _ = file.Close() }()

	if filepath.Base(file.Name()) == baseName {
		t.Errorf("expected a distinct collision-safe name, got %q", file.Name())
	}

	// The victim file must be untouched: O_EXCL never truncates.
	victim, err := os.ReadFile(preExisting)
	if err != nil {
		t.Fatalf("read victim: %v", err)
	}
	if string(victim) != "victim" {
		t.Errorf("collision-safe open truncated the pre-existing file: %q", victim)
	}
}

func TestUnwritableDebugDirErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory-as-path semantics differ on windows")
	}
	filePath := filepath.Join(t.TempDir(), "iam-a-file")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if _, err := NewSetup("exit-node", true, filePath); err == nil {
		t.Fatal("expected error opening debug file under a file-as-directory path")
	}
}

func TestRedactionHandler(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewTextHandler(&buf, nil)
	h := newRedactingHandler(base)
	h.RegisterSensitive("token", "tls_key")

	l := slog.New(h)
	l.Info("hello", "mode", "relay", "token", "SUPERSECRET", "tls_key", "PRIVATEKEY", "bytes", 1024)

	out := buf.String()
	for _, secret := range []string{"SUPERSECRET", "PRIVATEKEY"} {
		if strings.Contains(out, secret) {
			t.Errorf("secret %q leaked into log output", secret)
		}
	}
	if !strings.Contains(out, "[redacted]") {
		t.Error("expected redacted placeholder in output")
	}
	if !strings.Contains(out, "mode=relay") {
		t.Error("expected non-sensitive attr to survive")
	}
}

func TestRedactionCaseInsensitive(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewTextHandler(&buf, nil)
	h := newRedactingHandler(base)
	h.RegisterSensitive("Token")

	l := slog.New(h)
	l.Info("hi", "TOKEN", "should-hide", "token", "also-hide")

	out := buf.String()
	if strings.Contains(out, "should-hide") || strings.Contains(out, "also-hide") {
		t.Errorf("expected both case variants to be redacted, got: %q", out)
	}
}

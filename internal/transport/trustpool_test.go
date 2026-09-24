package transport

import (
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeCAPem persists a generated cert to a temp file as PEM so LoadTrustPool
// can be exercised end to end against real on-disk material.
func writeCAPem(t *testing.T) string {
	t.Helper()
	cert, _, _ := generateSelfSigned(t, "localhost")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]})
	dir := t.TempDir()
	path := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatalf("write ca pem: %v", err)
	}
	return path
}

func TestLoadTrustPool(t *testing.T) {
	path := writeCAPem(t)
	pool, err := LoadTrustPool(path)
	if err != nil {
		t.Fatalf("LoadTrustPool: %v", err)
	}
	if pool == nil {
		t.Fatal("want non-nil cert pool")
	}
}

func TestLoadTrustPoolGarbage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "garbage.pem")
	if err := os.WriteFile(path, []byte("not a pem file"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadTrustPool(path)
	if !errors.Is(err, ErrUntrustedCA) {
		t.Fatalf("want ErrUntrustedCA, got %v", err)
	}
}

func TestLoadTrustPoolMissingFile(t *testing.T) {
	_, err := LoadTrustPool(filepath.Join(t.TempDir(), "does-not-exist.pem"))
	if err == nil {
		t.Fatal("want error for missing file")
	}
}

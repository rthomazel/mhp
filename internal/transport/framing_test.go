package transport

import (
	"bufio"
	"bytes"
	"errors"
	"testing"

	"github.com/rthomazel/mhp/internal/config"
)

func TestReadWriteFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	data := []byte("hello frame world")
	if err := writeFrame(w, data); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	got, err := readFrame(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("round trip mismatch: got %q want %q", got, data)
	}
}

func TestReadFrameTooLarge(t *testing.T) {
	// Declare a body of 45056 bytes (> maxFrameBytes), supply none.
	var buf bytes.Buffer
	buf.WriteByte(0x01)
	buf.WriteByte(0x00)
	buf.WriteByte(0x01)
	buf.WriteByte(0x8c) // 45056 > maxFrameBytes
	_, err := readFrame(&buf)
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("want ErrFrameTooLarge, got %v", err)
	}
}

func TestReadFrameShortBody(t *testing.T) {
	// Declare a 4-byte body, supply only one.
	var buf bytes.Buffer
	buf.WriteByte(0x00)
	buf.WriteByte(0x00)
	buf.WriteByte(0x00)
	buf.WriteByte(0x04)
	buf.WriteByte('x')
	_, err := readFrame(&buf)
	if err == nil {
		t.Fatal("expected short-body error, got nil")
	}
}

func TestWriteFrameShortWrite(t *testing.T) {
	err := writeFrame(shortWriter{n: 1}, []byte("abcdef"))
	if !errors.Is(err, ErrShortWrite) {
		t.Fatalf("want ErrShortWrite, got %v", err)
	}
}

func TestReadWriteJSONRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	orig := Hello{Version: helloVersion, Role: config.ModeProxy, Token: "abc"}
	if err := writeJSON(&buf, orig); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	var got Hello
	if err := readJSON(&buf, &got); err != nil {
		t.Fatalf("readJSON: %v", err)
	}
	if got.Role != config.ModeProxy || got.Token != "abc" || got.Version != helloVersion {
		t.Fatalf("json round trip mismatch: %+v", got)
	}
}

func TestReadJSONMalformed(t *testing.T) {
	var buf bytes.Buffer
	if err := writeJSON(&buf, []byte("not-json")); err != nil {
		t.Fatalf("prep: %v", err)
	}
	var out Hello
	if err := readJSON(&buf, &out); !errors.Is(err, ErrMalformedRecord) {
		t.Fatalf("want ErrMalformedRecord, got %v", err)
	}
}

func TestWriteJSONMarshalError(t *testing.T) {
	var buf bytes.Buffer
	err := writeJSON(&buf, make(chan struct{}))
	if !errors.Is(err, ErrEncodeFailed) {
		t.Fatalf("want ErrEncodeFailed, got %v", err)
	}
}

// shortWriter caps each Write to n bytes so writeFrame detects a short write.
type shortWriter struct {
	n int
}

func (s shortWriter) Write(p []byte) (int, error) {
	if len(p) > s.n {
		p = p[:s.n]
	}
	return len(p), nil
}

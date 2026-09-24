package transport

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	frameHeaderLen = 4
	maxFrameBytes  = 4096
)

// ErrFrameTooLarge is returned when a frame declares a length beyond maxFrameBytes.
var ErrFrameTooLarge = errors.New("transport: frame too large")

// ErrShortWrite is returned when a frame's length prefix or body is only partially written.
var ErrShortWrite = errors.New("transport: short frame write")

// ErrMalformedRecord is returned when a frame is not valid JSON.
var ErrMalformedRecord = errors.New("transport: malformed record")

// ErrEncodeFailed is returned when a record cannot be serialized.
var ErrEncodeFailed = errors.New("transport: encode failed")

// readFrame pulls exactly one length-prefixed frame: a 4-byte big-endian
// uint32 declaring the body length, followed by that many body bytes.
func readFrame(r io.Reader) ([]byte, error) {
	var hdr [frameHeaderLen]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, fmt.Errorf("read frame header: %w", err)
	}
	length := binary.BigEndian.Uint32(hdr[:])
	if length > maxFrameBytes {
		return nil, fmt.Errorf("%w: %d", ErrFrameTooLarge, length)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("read frame body: %w", err)
	}
	return body, nil
}

// writeFrame emits a 4-byte big-endian length prefix followed by data, failing
// if the length is absurd or the socket short-writes.
func writeFrame(w io.Writer, data []byte) error {
	var buf [frameHeaderLen]byte
	binary.BigEndian.PutUint32(buf[:], uint32(len(data)))
	if _, err := w.Write(buf[:]); err != nil {
		return fmt.Errorf("write frame header: %w", err)
	}
	n, err := w.Write(data)
	if err != nil {
		return fmt.Errorf("write frame body: %w", err)
	}
	if n != len(data) {
		return fmt.Errorf("%w: wrote %d of %d", ErrShortWrite, n, len(data))
	}
	return nil
}

// readJSON reads one framed record and unmarshals it into target.
func readJSON(r io.Reader, target any) error {
	data, err := readFrame(r)
	if err != nil {
		return fmt.Errorf("read frame: %w", err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("%w: %w", ErrMalformedRecord, err)
	}
	return nil
}

// writeJSON marshals value, frames it, and writes it atomically.
func writeJSON(w io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrEncodeFailed, err)
	}
	return writeFrame(w, data)
}

package client

import (
	"bytes"
	"errors"
	"io"
	"math"
	"testing"
)

type trackingRC struct {
	r      io.Reader
	closed bool
}

func (t *trackingRC) Read(p []byte) (int, error) { return t.r.Read(p) }
func (t *trackingRC) Close() error {
	t.closed = true
	return nil
}

type errOnReadCloser struct {
	closed   bool
	readErr  error
	closeErr error
}

func (e *errOnReadCloser) Read(p []byte) (int, error) { return 0, e.readErr }
func (e *errOnReadCloser) Close() error {
	e.closed = true
	return e.closeErr
}

func TestReadAllAndCloseLimit_Closes(t *testing.T) {
	rc := &trackingRC{r: bytes.NewReader([]byte("abc"))}
	_, _ = ReadAllAndCloseLimit(rc, 10)
	if !rc.closed {
		t.Fatalf("expected body to be closed")
	}
}

func TestReadAllAndCloseLimit_NilBody(t *testing.T) {
	b, err := ReadAllAndCloseLimit(nil, 10)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if b != nil {
		t.Fatalf("expected nil bytes")
	}
}

func TestReadAllAndCloseLimit_TooLarge(t *testing.T) {
	rc := &trackingRC{r: bytes.NewReader([]byte("abcd"))}
	_, err := ReadAllAndCloseLimit(rc, 3)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("got err=%v, want ErrBodyTooLarge", err)
	}
	if !rc.closed {
		t.Fatalf("expected body to be closed")
	}
}

func TestReadAllAndCloseLimit_LimitZero(t *testing.T) {
	rc := &trackingRC{r: bytes.NewReader([]byte("a"))}
	_, err := ReadAllAndCloseLimit(rc, 0)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("got err=%v, want ErrBodyTooLarge", err)
	}
	if !rc.closed {
		t.Fatalf("expected body to be closed")
	}
}

func TestDrainAndClose_Closes(t *testing.T) {
	rc := &trackingRC{r: bytes.NewReader(make([]byte, 1024))}
	if err := DrainAndClose(rc, 10); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !rc.closed {
		t.Fatalf("expected body to be closed")
	}
}

func TestDrainAndClose_MaxNonPositive_Closes(t *testing.T) {
	rc := &trackingRC{r: bytes.NewReader([]byte("abc"))}
	if err := DrainAndClose(rc, 0); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !rc.closed {
		t.Fatalf("expected body to be closed")
	}
}

func TestDrainAndClose_ReadErrorPreferredOverCloseError(t *testing.T) {
	readErr := errors.New("read boom")
	closeErr := errors.New("close boom")
	rc := &errOnReadCloser{readErr: readErr, closeErr: closeErr}
	err := DrainAndClose(rc, 10)
	if err == nil || err.Error() != readErr.Error() {
		t.Fatalf("got err=%v, want read error %v", err, readErr)
	}
	if !rc.closed {
		t.Fatalf("expected body to be closed")
	}
}

func TestReadAllAndCloseLimit_MaxInt64_NoOverflow(t *testing.T) {
	rc := &trackingRC{r: bytes.NewReader([]byte("abc"))}
	b, err := ReadAllAndCloseLimit(rc, math.MaxInt64)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if string(b) != "abc" {
		t.Fatalf("got %q, want %q", string(b), "abc")
	}
	if !rc.closed {
		t.Fatalf("expected body to be closed")
	}
}

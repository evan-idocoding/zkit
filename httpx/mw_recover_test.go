package httpx

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecover_Writes500WhenNoResponseWritten(t *testing.T) {
	h := Chain(Recover()).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
	}
	if !strings.Contains(rr.Body.String(), http.StatusText(http.StatusInternalServerError)) {
		t.Fatalf("expected response body to contain %q, got %q", http.StatusText(http.StatusInternalServerError), rr.Body.String())
	}
}

func TestRecover_DoesNotOverrideWhenAlreadyWroteHeader(t *testing.T) {
	h := Chain(Recover()).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		panic("boom")
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rr.Code)
	}
}

func TestRecover_ErrAbortHandlerRepanics(t *testing.T) {
	h := Chain(Recover()).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)

	defer func() {
		p := recover()
		if p == nil {
			t.Fatalf("expected panic")
		}
		if p != http.ErrAbortHandler {
			t.Fatalf("expected panic value %v, got %v", http.ErrAbortHandler, p)
		}
	}()
	h.ServeHTTP(rr, req)
}

func TestRecover_ErrAbortHandlerDoesNotCallOnPanic(t *testing.T) {
	called := false
	h := Chain(Recover(WithOnPanic(func(r *http.Request, info RecoverInfo) {
		called = true
	}))).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)

	defer func() {
		p := recover()
		if p == nil {
			t.Fatalf("expected panic")
		}
		if p != http.ErrAbortHandler {
			t.Fatalf("expected panic value %v, got %v", http.ErrAbortHandler, p)
		}
		if called {
			t.Fatalf("expected OnPanic not to be called for http.ErrAbortHandler")
		}
	}()
	h.ServeHTTP(rr, req)
}

func TestRecover_OnPanicCalled(t *testing.T) {
	var (
		called bool
		gotVal any
		gotStk []byte
	)
	h := Chain(Recover(WithOnPanic(func(r *http.Request, info RecoverInfo) {
		called = true
		gotVal = info.Value
		gotStk = info.Stack
	}))).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	h.ServeHTTP(rr, req)

	if !called {
		t.Fatalf("expected OnPanic to be called")
	}
	if gotVal != "boom" {
		t.Fatalf("expected panic value %q, got %v", "boom", gotVal)
	}
	if len(gotStk) == 0 {
		t.Fatalf("expected non-empty stack")
	}
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
	}
}

func TestRecover_PanicsOnNilNext(t *testing.T) {
	mw := Recover()
	defer func() {
		if p := recover(); p == nil {
			t.Fatalf("expected panic")
		}
	}()
	_ = mw(nil)
}

func TestRecover_OnPanicPanics_IsSwallowed(t *testing.T) {
	h := Chain(Recover(WithOnPanic(func(r *http.Request, info RecoverInfo) {
		panic("handler boom")
	}))).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
	}
}

func TestRecover_DoesNotOverrideWhenAlreadyWroteBody(t *testing.T) {
	h := Chain(Recover()).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
		panic("boom")
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	h.ServeHTTP(rr, req)

	// Body write starts the response; Recover must NOT append a 500 body.
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if rr.Body.String() != "ok" {
		t.Fatalf("expected body %q, got %q", "ok", rr.Body.String())
	}
}

func TestRecover_ReadFromFallbackMarksResponseStarted(t *testing.T) {
	h := Chain(Recover()).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(w, strings.NewReader("ok"))
		panic("boom")
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	h.ServeHTTP(rr, req)

	// The handler already wrote to the response; Recover must NOT append a 500 body.
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if rr.Body.String() != "ok" {
		t.Fatalf("expected body %q, got %q", "ok", rr.Body.String())
	}
}

func TestRecover_FlushMarksResponseStarted(t *testing.T) {
	uw := &testRWFlusher{}
	h := Chain(Recover()).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush()
		panic("boom")
	}))
	h.ServeHTTP(uw, httptest.NewRequest(http.MethodGet, "http://example.test/", nil))

	// Flush indicates the response has started; Recover should not write 500.
	if uw.code == http.StatusInternalServerError {
		t.Fatalf("expected Recover not to write 500 after Flush")
	}
	if uw.body.Len() != 0 {
		t.Fatalf("expected empty body, got %q", uw.body.String())
	}
}

func TestRecover_HijackMarksResponseStarted(t *testing.T) {
	uw := &testRWHijacker{}
	h := Chain(Recover()).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Fatalf("unexpected hijack error: %v", err)
		}
		panic("boom")
	}))
	h.ServeHTTP(uw, httptest.NewRequest(http.MethodGet, "http://example.test/", nil))

	// After hijack, Recover should not attempt to write an HTTP 500.
	if uw.code == http.StatusInternalServerError {
		t.Fatalf("expected Recover not to write 500 after Hijack")
	}
	if uw.body.Len() != 0 {
		t.Fatalf("expected empty body, got %q", uw.body.String())
	}
}

type testRWFlusher struct {
	hdr  http.Header
	code int
	body bytes.Buffer
}

func (w *testRWFlusher) Header() http.Header {
	if w.hdr == nil {
		w.hdr = make(http.Header)
	}
	return w.hdr
}
func (w *testRWFlusher) WriteHeader(statusCode int) {
	if w.code == 0 {
		w.code = statusCode
	}
}
func (w *testRWFlusher) Write(p []byte) (int, error) {
	if w.code == 0 {
		w.code = http.StatusOK
	}
	return w.body.Write(p)
}
func (w *testRWFlusher) Flush() {}

type testRWHijacker struct {
	hdr  http.Header
	code int
	body bytes.Buffer
}

func (w *testRWHijacker) Header() http.Header {
	if w.hdr == nil {
		w.hdr = make(http.Header)
	}
	return w.hdr
}
func (w *testRWHijacker) WriteHeader(statusCode int) {
	if w.code == 0 {
		w.code = statusCode
	}
}
func (w *testRWHijacker) Write(p []byte) (int, error) {
	if w.code == 0 {
		w.code = http.StatusOK
	}
	return w.body.Write(p)
}
func (w *testRWHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c1, c2 := net.Pipe()
	_ = c2.Close()
	return c1, bufio.NewReadWriter(bufio.NewReader(c1), bufio.NewWriter(c1)), nil
}

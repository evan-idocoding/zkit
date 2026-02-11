package httpx

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestBodyLimit_ContentLengthRejectsEarlyAndCallsOnReject(t *testing.T) {
	var downstreamCalled bool
	var onCalled int32
	var got BodyLimitInfo

	h := BodyLimit(5, WithOnReject(func(r *http.Request, info BodyLimitInfo) {
		atomic.AddInt32(&onCalled, 1)
		got = info
	}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downstreamCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "http://example.test/", bytes.NewBufferString("123456"))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if downstreamCalled {
		t.Fatalf("expected downstream not called on early reject")
	}
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status %d, got %d", http.StatusRequestEntityTooLarge, rr.Code)
	}
	if rr.Header().Get("Connection") != "close" {
		t.Fatalf("expected response header Connection=close, got %q", rr.Header().Get("Connection"))
	}
	if atomic.LoadInt32(&onCalled) != 1 {
		t.Fatalf("expected OnReject called once, got %d", atomic.LoadInt32(&onCalled))
	}
	if got.Source != BodyLimitSourceContentLength {
		t.Fatalf("expected Source=%q, got %q", BodyLimitSourceContentLength, got.Source)
	}
	if got.Limit != 5 {
		t.Fatalf("expected Limit=5, got %d", got.Limit)
	}
	if got.ContentLength != 6 {
		t.Fatalf("expected ContentLength=6, got %d", got.ContentLength)
	}
}

func TestBodyLimit_ContentLengthRejectsEarly_ClosesRequestBody(t *testing.T) {
	rc := &testClosingRC{}
	req := httptest.NewRequest(http.MethodPost, "http://example.test/", rc)
	// Force Content-Length known and above limit (avoid relying on body semantics).
	req.ContentLength = 6

	h := BodyLimit(5)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("downstream should not be called")
	}))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status %d, got %d", http.StatusRequestEntityTooLarge, rr.Code)
	}
	if atomic.LoadInt32(&rc.closed) != 1 {
		t.Fatalf("expected request body to be closed on early reject")
	}
}

type testClosingRC struct {
	closed int32
}

func (c *testClosingRC) Read(p []byte) (int, error) { return 0, io.EOF }

func (c *testClosingRC) Close() error {
	atomic.StoreInt32(&c.closed, 1)
	return nil
}

func TestBodyLimit_ReadExceedDoesNotAutoRespondButCallsOnReject(t *testing.T) {
	var onCalled int32
	var got BodyLimitInfo
	var sawMaxBytes bool

	h := BodyLimit(5, WithOnReject(func(r *http.Request, info BodyLimitInfo) {
		atomic.AddInt32(&onCalled, 1)
		got = info
	}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			sawMaxBytes = true
			// BodyLimit does not auto-respond for read-time exceed (per design).
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Wrap with NopCloser so httptest.NewRequest does not auto-populate ContentLength.
	req := httptest.NewRequest(http.MethodPost, "http://example.test/", io.NopCloser(bytes.NewBufferString("123456")))
	if req.ContentLength != -1 {
		t.Fatalf("expected ContentLength=-1 for unknown length, got %d", req.ContentLength)
	}

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if !sawMaxBytes {
		t.Fatalf("expected downstream to see *http.MaxBytesError")
	}
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected downstream-controlled status %d, got %d", http.StatusBadRequest, rr.Code)
	}
	if atomic.LoadInt32(&onCalled) != 1 {
		t.Fatalf("expected OnReject called once, got %d", atomic.LoadInt32(&onCalled))
	}
	if got.Source != BodyLimitSourceRead {
		t.Fatalf("expected Source=%q, got %q", BodyLimitSourceRead, got.Source)
	}
	if got.Limit != 5 {
		t.Fatalf("expected Limit=5, got %d", got.Limit)
	}
	if got.ContentLength != -1 {
		t.Fatalf("expected ContentLength=-1, got %d", got.ContentLength)
	}
}

func TestBodyLimit_LimitFuncOverridesDefault_ReadPath(t *testing.T) {
	var onCalled int32
	var got BodyLimitInfo
	var sawMaxBytes bool

	h := BodyLimit(100,
		WithLimitFunc(func(r *http.Request) (int64, bool) { return 5, true }),
		WithOnReject(func(r *http.Request, info BodyLimitInfo) {
			atomic.AddInt32(&onCalled, 1)
			got = info
		}),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			sawMaxBytes = true
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "http://example.test/", io.NopCloser(bytes.NewBufferString("123456")))
	if req.ContentLength != -1 {
		t.Fatalf("expected ContentLength=-1 for unknown length, got %d", req.ContentLength)
	}

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if !sawMaxBytes {
		t.Fatalf("expected downstream to see *http.MaxBytesError")
	}
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected downstream-controlled status %d, got %d", http.StatusBadRequest, rr.Code)
	}
	if atomic.LoadInt32(&onCalled) != 1 {
		t.Fatalf("expected OnReject called once, got %d", atomic.LoadInt32(&onCalled))
	}
	if got.Source != BodyLimitSourceRead {
		t.Fatalf("expected Source=%q, got %q", BodyLimitSourceRead, got.Source)
	}
	if got.Limit != 5 {
		t.Fatalf("expected Limit=5, got %d", got.Limit)
	}
	if got.ContentLength != -1 {
		t.Fatalf("expected ContentLength=-1, got %d", got.ContentLength)
	}
}

func TestBodyLimit_LimitFuncSkip(t *testing.T) {
	h := BodyLimit(5, WithLimitFunc(func(r *http.Request) (int64, bool) {
		return 0, false
	}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("unexpected read error: %v", err)
		}
		if len(b) != 6 {
			t.Fatalf("expected len=6, got %d", len(b))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "http://example.test/", bytes.NewBufferString("123456"))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestBodyLimit_Skipped_DoesNotCallOnReject(t *testing.T) {
	var onCalled int32
	h := BodyLimit(5,
		WithLimitFunc(func(r *http.Request) (int64, bool) { return 0, false }), // skip
		WithOnReject(func(r *http.Request, info BodyLimitInfo) {
			atomic.AddInt32(&onCalled, 1)
		}),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("unexpected read error: %v", err)
		}
		if len(b) != 6 {
			t.Fatalf("expected len=6, got %d", len(b))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "http://example.test/", bytes.NewBufferString("123456"))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if atomic.LoadInt32(&onCalled) != 0 {
		t.Fatalf("expected OnReject not called when skipped, got %d", atomic.LoadInt32(&onCalled))
	}
}

func TestBodyLimit_LimitFuncZeroSkips(t *testing.T) {
	h := BodyLimit(5, WithLimitFunc(func(r *http.Request) (int64, bool) {
		return 0, true
	}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("unexpected read error: %v", err)
		}
		if len(b) != 6 {
			t.Fatalf("expected len=6, got %d", len(b))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "http://example.test/", bytes.NewBufferString("123456"))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestBodyLimit_DoesNotRejectWhenContentLengthEqualsLimit(t *testing.T) {
	var onCalled int32
	h := BodyLimit(6, WithOnReject(func(r *http.Request, info BodyLimitInfo) {
		atomic.AddInt32(&onCalled, 1)
	}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("unexpected read error: %v", err)
		}
		if len(b) != 6 {
			t.Fatalf("expected len=6, got %d", len(b))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "http://example.test/", bytes.NewBufferString("123456"))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if atomic.LoadInt32(&onCalled) != 0 {
		t.Fatalf("expected OnReject not called when within limit, got %d", atomic.LoadInt32(&onCalled))
	}
}

func TestBodyLimit_UnknownLengthWithinLimit_DoesNotCallOnReject(t *testing.T) {
	var onCalled int32
	h := BodyLimit(5, WithOnReject(func(r *http.Request, info BodyLimitInfo) {
		atomic.AddInt32(&onCalled, 1)
	}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("unexpected read error: %v", err)
		}
		if len(b) != 5 {
			t.Fatalf("expected len=5, got %d", len(b))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "http://example.test/", io.NopCloser(bytes.NewBufferString("12345")))
	if req.ContentLength != -1 {
		t.Fatalf("expected ContentLength=-1 for unknown length, got %d", req.ContentLength)
	}

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	if atomic.LoadInt32(&onCalled) != 0 {
		t.Fatalf("expected OnReject not called when within limit, got %d", atomic.LoadInt32(&onCalled))
	}
}

func TestBodyLimit_ZeroOrNegativeLimitSkips(t *testing.T) {
	h := BodyLimit(0)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("unexpected read error: %v", err)
		}
		if len(b) != 6 {
			t.Fatalf("expected len=6, got %d", len(b))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "http://example.test/", bytes.NewBufferString("123456"))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestBodyLimit_OnRejectPanicIsSwallowed(t *testing.T) {
	h := BodyLimit(5, WithOnReject(func(r *http.Request, info BodyLimitInfo) {
		panic("boom")
	}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("downstream should not be called")
	}))

	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("expected panic swallowed, got %v", p)
		}
	}()

	req := httptest.NewRequest(http.MethodPost, "http://example.test/", bytes.NewBufferString("123456"))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status %d, got %d", http.StatusRequestEntityTooLarge, rr.Code)
	}
}

func TestBodyLimit_OnRejectPanicIsSwallowed_OnReadPath(t *testing.T) {
	h := BodyLimit(5, WithOnReject(func(r *http.Request, info BodyLimitInfo) {
		panic("boom")
	}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusBadRequest)
	}))

	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("expected panic swallowed, got %v", p)
		}
	}()

	req := httptest.NewRequest(http.MethodPost, "http://example.test/", io.NopCloser(bytes.NewBufferString("123456")))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected downstream-controlled status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestBodyLimit_PanicsOnNilNext(t *testing.T) {
	mw := BodyLimit(5)
	defer func() {
		if p := recover(); p == nil {
			t.Fatalf("expected panic")
		}
	}()
	_ = mw(nil)
}

func TestBodyLimit_PanicsOnNilRequest(t *testing.T) {
	h := BodyLimit(5)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("should not reach next handler")
	}))

	defer func() {
		if p := recover(); p == nil {
			t.Fatalf("expected panic")
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), nil)
}

func TestBodyLimit_NilBodyIsTreatedAsNoBody(t *testing.T) {
	h := BodyLimit(5)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("unexpected read error: %v", err)
		}
		if len(b) != 0 {
			t.Fatalf("expected empty body, got %d bytes", len(b))
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "http://example.test/", nil)
	req.Body = nil // simulate unexpected nil Body
	req.ContentLength = 0

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func ExampleBodyLimit() {
	// Global default limit (e.g., for API handlers).
	apiLimit := BodyLimit(1<<20 /* 1 MiB */, WithOnReject(func(r *http.Request, info BodyLimitInfo) {
		_ = r
		_ = info
		// Record metrics / logs here (do NOT write response).
	}))

	// Per-request override/skip (e.g., tune at runtime; skip long-lived or special endpoints).
	conditional := BodyLimit(1<<20, WithLimitFunc(func(r *http.Request) (int64, bool) {
		if r.URL != nil && r.URL.Path == "/upload/large" {
			return 0, false // skip
		}
		return 1 << 20, true
	}))

	_ = apiLimit
	_ = conditional
}

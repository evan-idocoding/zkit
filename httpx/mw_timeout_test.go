package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestTimeout_RespectsEarlierParentDeadline(t *testing.T) {
	parentDeadline := time.Now().Add(50 * time.Millisecond)
	parentCtx, cancel := context.WithDeadline(context.Background(), parentDeadline)
	defer cancel()

	var gotDeadline time.Time
	var gotOK bool
	h := Timeout(5 * time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotDeadline, gotOK = r.Context().Deadline()
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil).WithContext(parentCtx)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !gotOK {
		t.Fatalf("expected downstream to have a deadline")
	}
	if !gotDeadline.Equal(parentDeadline) {
		t.Fatalf("expected deadline=%v, got %v", parentDeadline, gotDeadline)
	}
}

func TestTimeout_TightensLaterParentDeadline(t *testing.T) {
	parentDeadline := time.Now().Add(5 * time.Second)
	parentCtx, cancel := context.WithDeadline(context.Background(), parentDeadline)
	defer cancel()

	var gotDeadline time.Time
	var gotOK bool
	start := time.Now()
	timeout := 40 * time.Millisecond

	h := Timeout(timeout)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotDeadline, gotOK = r.Context().Deadline()
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil).WithContext(parentCtx)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !gotOK {
		t.Fatalf("expected downstream to have a deadline")
	}
	if !gotDeadline.Before(parentDeadline) {
		t.Fatalf("expected tightened deadline before parent deadline; parent=%v got=%v", parentDeadline, gotDeadline)
	}
	// Allow some scheduling jitter; we only check it's not wildly off.
	if gotDeadline.After(start.Add(timeout + 250*time.Millisecond)) {
		t.Fatalf("expected deadline not far in the future; start=%v timeout=%v got=%v", start, timeout, gotDeadline)
	}
}

func TestTimeout_TimeoutFuncSkip(t *testing.T) {
	var gotOK bool
	h := Timeout(1*time.Second, WithTimeoutFunc(func(r *http.Request) (time.Duration, bool) {
		return 0, false
	}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, gotOK = r.Context().Deadline()
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if gotOK {
		t.Fatalf("expected no derived deadline when skipped")
	}
}

func TestTimeout_TimeoutFuncZeroDurationSkips(t *testing.T) {
	var gotOK bool
	h := Timeout(1*time.Second, WithTimeoutFunc(func(r *http.Request) (time.Duration, bool) {
		return 0, true
	}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, gotOK = r.Context().Deadline()
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if gotOK {
		t.Fatalf("expected no derived deadline when timeoutFunc returns d<=0")
	}
}

func TestTimeout_ZeroOrNegativeTimeoutSkips(t *testing.T) {
	var gotOK bool
	h := Timeout(0)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, gotOK = r.Context().Deadline()
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if gotOK {
		t.Fatalf("expected no derived deadline when timeout<=0")
	}
}

func TestTimeout_WithNowControlsDeadlineAndElapsed(t *testing.T) {
	t0 := time.Now()
	var calls int32
	now := func() time.Time {
		// 1st call: start
		// 2nd+ call: used for elapsed calculation
		if atomic.AddInt32(&calls, 1) == 1 {
			return t0
		}
		return t0.Add(25 * time.Millisecond)
	}

	var gotDeadline time.Time
	var gotOK bool
	var gotInfo TimeoutInfo
	var onCalled int32

	h := Timeout(10*time.Millisecond,
		WithNow(now),
		WithOnTimeout(func(r *http.Request, info TimeoutInfo) {
			atomic.AddInt32(&onCalled, 1)
			gotInfo = info
		}),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotDeadline, gotOK = r.Context().Deadline()
		<-r.Context().Done()
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !gotOK {
		t.Fatalf("expected downstream to have a deadline")
	}
	want := t0.Add(10 * time.Millisecond)
	if !gotDeadline.Equal(want) {
		t.Fatalf("expected deadline=%v, got %v", want, gotDeadline)
	}
	if atomic.LoadInt32(&onCalled) != 1 {
		t.Fatalf("expected OnTimeout called once, got %d", atomic.LoadInt32(&onCalled))
	}
	if gotInfo.Deadline.IsZero() || !gotInfo.Deadline.Equal(want) {
		t.Fatalf("expected TimeoutInfo.Deadline=%v, got %v", want, gotInfo.Deadline)
	}
	if gotInfo.Timeout != 10*time.Millisecond {
		t.Fatalf("expected TimeoutInfo.Timeout=%v, got %v", 10*time.Millisecond, gotInfo.Timeout)
	}
	if gotInfo.Elapsed != 25*time.Millisecond {
		t.Fatalf("expected TimeoutInfo.Elapsed=%v, got %v", 25*time.Millisecond, gotInfo.Elapsed)
	}
}

func TestTimeout_OnTimeoutCalledOnDeadlineExceeded(t *testing.T) {
	var called int32
	h := Timeout(15*time.Millisecond, WithOnTimeout(func(r *http.Request, info TimeoutInfo) {
		atomic.AddInt32(&called, 1)
		if r.Context().Err() != context.DeadlineExceeded {
			t.Fatalf("expected r.Context().Err()==DeadlineExceeded, got %v", r.Context().Err())
		}
		if info.Timeout <= 0 {
			t.Fatalf("expected info.Timeout > 0")
		}
		if info.Deadline.IsZero() {
			t.Fatalf("expected info.Deadline set")
		}
	}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if atomic.LoadInt32(&called) != 1 {
		t.Fatalf("expected OnTimeout called once, got %d", atomic.LoadInt32(&called))
	}
}

func TestTimeout_OnTimeoutNotCalledOnCancel(t *testing.T) {
	parentCtx, cancel := context.WithCancel(context.Background())
	cancel()

	var called int32
	h := Timeout(5*time.Second, WithOnTimeout(func(r *http.Request, info TimeoutInfo) {
		atomic.AddInt32(&called, 1)
	}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil).WithContext(parentCtx)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if atomic.LoadInt32(&called) != 0 {
		t.Fatalf("expected OnTimeout not called for context.Canceled, got %d", atomic.LoadInt32(&called))
	}
}

func TestTimeout_OnTimeoutNotCalledWhenParentDeadlineIsEarlier(t *testing.T) {
	// Parent already has a much earlier deadline, so Timeout must keep the parent context
	// and should not call OnTimeout (per documented trigger condition).
	parentCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	var called int32
	h := Timeout(5*time.Second, WithOnTimeout(func(r *http.Request, info TimeoutInfo) {
		atomic.AddInt32(&called, 1)
	}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil).WithContext(parentCtx)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if atomic.LoadInt32(&called) != 0 {
		t.Fatalf("expected OnTimeout not called when parent deadline is earlier, got %d", atomic.LoadInt32(&called))
	}
}

func TestTimeout_OnTimeoutPanicIsSwallowed(t *testing.T) {
	h := Timeout(10*time.Millisecond, WithOnTimeout(func(r *http.Request, info TimeoutInfo) {
		panic("boom")
	}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))

	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("expected panic swallowed, got %v", p)
		}
	}()

	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)
}

func ExampleTimeout() {
	// Global default timeout (e.g., for API handlers).
	apiTimeout := Timeout(2*time.Second, WithOnTimeout(func(r *http.Request, info TimeoutInfo) {
		_ = r
		_ = info
		// Record metrics / logs here (do NOT write response).
	}))

	// Per-request override/skip (e.g., skip long-lived streaming endpoints).
	conditional := Timeout(2*time.Second, WithTimeoutFunc(func(r *http.Request) (time.Duration, bool) {
		if r.URL != nil && r.URL.Path == "/sse" {
			return 0, false // skip
		}
		return 2 * time.Second, true
	}))

	_ = apiTimeout
	_ = conditional
}

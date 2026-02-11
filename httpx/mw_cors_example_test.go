package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
)

func ExampleCORS_default() {
	// Default: allow any Origin (reflected), allow credentials, short-circuit preflight.
	h := CORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Set("Origin", "https://frontend.example")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	_ = rr.Result()
}

func ExampleCORS_matchFunc() {
	// Apply CORS only to a subtree (e.g. debug endpoints).
	h := CORS(WithMatchFunc(func(r *http.Request) bool {
		return strings.HasPrefix(r.URL.Path, "/debug/")
	}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.test/debug/ping", nil)
	req.Header.Set("Origin", "https://frontend.example")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	_ = rr.Result()
}

func ExampleCORS_enabledFunc() {
	// Runtime toggle (e.g. wired to tuning/feature flag).
	var enabled int32
	atomic.StoreInt32(&enabled, 1)

	h := CORS(WithEnabledFunc(func(r *http.Request) bool {
		return atomic.LoadInt32(&enabled) == 1
	}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Set("Origin", "https://frontend.example")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	_ = rr.Result()
}

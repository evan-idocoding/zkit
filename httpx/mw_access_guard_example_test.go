package httpx

import (
	"net/http"
	"net/http/httptest"
)

func ExampleAccessGuard_tokenOnly() {
	// Token-only guard: no RealIP middleware required.
	set := NewAtomicTokenSet()
	set.Update([]string{"tokenA"})

	h := AccessGuard(
		WithTokenSet(set),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Set(DefaultAccessGuardTokenHeader, "tokenA")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	_ = rr.Result()
}

func ExampleAccessGuard_tokenAndIP() {
	// Token + IP allowlist.
	h := Chain(
		RealIP(WithTrustedProxies([]string{"10.0.0.0/8"})),
		AccessGuard(
			WithTokens([]string{"tokenA"}),
			WithIPAllowList([]string{"203.0.113.0/24"}),
		),
	).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.RemoteAddr = "10.0.0.1:12345"                 // trusted proxy
	req.Header.Set("X-Forwarded-For", "203.0.113.50") // real client
	req.Header.Set(DefaultAccessGuardTokenHeader, "tokenA")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	_ = rr.Result()
}

package admin_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/evan-idocoding/zkit/admin"
)

func ExampleNew_healthz() {
	h := admin.New(
		admin.EnableHealthz(admin.HealthzSpec{Guard: admin.AllowAll()}),
	)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://admin.test/healthz", nil)
	h.ServeHTTP(rr, req)

	fmt.Print(rr.Body.String())

	// Output:
	// ok
}

func ExampleTokens() {
	g := admin.Tokens([]string{"t"})
	h := admin.New(
		admin.EnableHealthz(admin.HealthzSpec{Guard: g}),
	)

	rr1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "http://admin.test/healthz", nil)
	h.ServeHTTP(rr1, req1)

	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "http://admin.test/healthz", nil)
	req2.Header.Set(admin.DefaultTokenHeader, "t")
	h.ServeHTTP(rr2, req2)

	fmt.Println(rr1.Code, rr2.Code)

	// Output:
	// 403 200
}

func ExampleWithRealIP() {
	// Without configuring TrustedProxies, RealIP headers are ignored (default-safe),
	// so IPAllowList checks fall back to RemoteAddr.
	h0 := admin.New(
		admin.EnableHealthz(admin.HealthzSpec{Guard: admin.IPAllowList("10.0.0.0/8")}),
	)

	// With TrustedProxies, RealIP headers can be trusted when the direct client is a trusted proxy.
	h1 := admin.New(
		admin.WithRealIP(admin.RealIPSpec{TrustedProxies: []string{"192.168.0.0/16"}}),
		admin.EnableHealthz(admin.HealthzSpec{Guard: admin.IPAllowList("10.0.0.0/8")}),
	)

	// Simulate a request coming from a trusted proxy, forwarding a real client in 10.0.0.0/8.
	req := httptest.NewRequest(http.MethodGet, "http://admin.test/healthz", nil)
	req.RemoteAddr = "192.168.0.1:1234"
	req.Header.Set("X-Forwarded-For", "10.1.2.3")

	rr0 := httptest.NewRecorder()
	h0.ServeHTTP(rr0, req)
	rr1 := httptest.NewRecorder()
	h1.ServeHTTP(rr1, req)

	fmt.Println(rr0.Code, rr1.Code)

	// Output:
	// 403 200
}

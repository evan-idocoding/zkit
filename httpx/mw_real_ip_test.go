package httpx

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRealIP_NoTrustedProxies(t *testing.T) {
	// Without trusted proxies, should always use RemoteAddr.
	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		xRealIP    string
		wantIP     string
	}{
		{
			name:       "uses RemoteAddr directly",
			remoteAddr: "203.0.113.50:12345",
			xff:        "10.0.0.1",
			xRealIP:    "10.0.0.2",
			wantIP:     "203.0.113.50",
		},
		{
			name:       "RemoteAddr without port",
			remoteAddr: "203.0.113.50",
			wantIP:     "203.0.113.50",
		},
		{
			name:       "IPv6 RemoteAddr",
			remoteAddr: "[2001:db8::1]:12345",
			xff:        "10.0.0.1",
			wantIP:     "2001:db8::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotIP net.IP
			handler := RealIP()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotIP, _ = RealIPFromRequest(r)
			}))

			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}

			handler.ServeHTTP(httptest.NewRecorder(), req)

			if gotIP.String() != tt.wantIP {
				t.Errorf("got IP %v, want %v", gotIP, tt.wantIP)
			}
		})
	}
}

func TestRealIP_WithTrustedProxies(t *testing.T) {
	tests := []struct {
		name           string
		trustedProxies []string
		remoteAddr     string
		xff            string
		xRealIP        string
		wantIP         string
	}{
		{
			name:           "trusted proxy with XFF",
			trustedProxies: []string{"10.0.0.0/8"},
			remoteAddr:     "10.0.0.1:12345",
			xff:            "203.0.113.50",
			wantIP:         "203.0.113.50",
		},
		{
			name:           "untrusted direct IP ignores headers",
			trustedProxies: []string{"10.0.0.0/8"},
			remoteAddr:     "203.0.113.99:12345",
			xff:            "1.2.3.4",
			wantIP:         "203.0.113.99",
		},
		{
			name:           "XFF chain with multiple proxies",
			trustedProxies: []string{"10.0.0.0/8", "192.168.0.0/16"},
			remoteAddr:     "10.0.0.1:12345",
			xff:            "203.0.113.50, 10.0.0.2, 192.168.1.1",
			wantIP:         "203.0.113.50",
		},
		{
			name:           "XFF chain all trusted returns nil then uses directIP",
			trustedProxies: []string{"10.0.0.0/8"},
			remoteAddr:     "10.0.0.1:12345",
			xff:            "10.0.0.2, 10.0.0.3",
			wantIP:         "10.0.0.1",
		},
		{
			name:           "X-Real-IP fallback when XFF empty",
			trustedProxies: []string{"10.0.0.0/8"},
			remoteAddr:     "10.0.0.1:12345",
			xff:            "",
			xRealIP:        "203.0.113.50",
			wantIP:         "203.0.113.50",
		},
		{
			name:           "XFF takes priority over X-Real-IP",
			trustedProxies: []string{"10.0.0.0/8"},
			remoteAddr:     "10.0.0.1:12345",
			xff:            "203.0.113.50",
			xRealIP:        "1.2.3.4",
			wantIP:         "203.0.113.50",
		},
		{
			name:           "single IP CIDR",
			trustedProxies: []string{"10.0.0.1"},
			remoteAddr:     "10.0.0.1:12345",
			xff:            "203.0.113.50",
			wantIP:         "203.0.113.50",
		},
		{
			name:           "IPv6 trusted proxy",
			trustedProxies: []string{"::1/128"},
			remoteAddr:     "[::1]:12345",
			xff:            "2001:db8::1",
			wantIP:         "2001:db8::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotIP net.IP
			handler := RealIP(
				WithTrustedProxies(tt.trustedProxies),
			)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotIP, _ = RealIPFromRequest(r)
			}))

			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}

			handler.ServeHTTP(httptest.NewRecorder(), req)

			if gotIP.String() != tt.wantIP {
				t.Errorf("got IP %v, want %v", gotIP, tt.wantIP)
			}
		})
	}
}

func TestRealIP_XFFInvalidIP(t *testing.T) {
	// If XFF contains an invalid IP, stop processing (security measure).
	var gotIP net.IP
	var gotOK bool
	handler := RealIP(
		WithTrustedProxies([]string{"10.0.0.0/8"}),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP, gotOK = RealIPFromRequest(r)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.50, invalid, 10.0.0.2")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	// Should fall back to direct IP when invalid IP encountered.
	if gotIP.String() != "10.0.0.1" {
		t.Errorf("got IP %v, want 10.0.0.1 (fallback)", gotIP)
	}
	if !gotOK {
		t.Error("expected gotOK to be true")
	}
}

func TestRealIP_XFFInvalidIP_SkipPolicy(t *testing.T) {
	// With skip policy, invalid tokens are ignored and scanning continues left.
	var gotIP net.IP
	handler := RealIP(
		WithTrustedProxies([]string{"10.0.0.0/8"}),
		WithXFFInvalidPolicy(XFFInvalidSkip),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP, _ = RealIPFromRequest(r)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.50, invalid, 10.0.0.2")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	// 10.0.0.2 is trusted, invalid is skipped, then 203.0.113.50 is chosen.
	if gotIP.String() != "203.0.113.50" {
		t.Errorf("got IP %v, want 203.0.113.50", gotIP)
	}
}

func TestRealIP_XFFUnknownToken_SkipUnknownPolicy(t *testing.T) {
	// With skip-unknown policy, only "unknown" tokens are ignored; other invalid values stop.
	var gotIP net.IP
	handler := RealIP(
		WithTrustedProxies([]string{"10.0.0.0/8"}),
		WithXFFInvalidPolicy(XFFInvalidSkipUnknown),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP, _ = RealIPFromRequest(r)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.50, unknown, 10.0.0.2")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if gotIP.String() != "203.0.113.50" {
		t.Errorf("got IP %v, want 203.0.113.50", gotIP)
	}
}

func TestRealIP_CustomHeaders(t *testing.T) {
	var gotIP net.IP
	handler := RealIP(
		WithTrustedProxies([]string{"10.0.0.0/8"}),
		WithTrustedHeaders([]string{"X-Real-IP", "X-Forwarded-For"}), // Reversed order
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP, _ = RealIPFromRequest(r)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "1.1.1.1")
	req.Header.Set("X-Real-IP", "2.2.2.2")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	// X-Real-IP should take priority now.
	if gotIP.String() != "2.2.2.2" {
		t.Errorf("got IP %v, want 2.2.2.2", gotIP)
	}
}

func TestRealIP_EmptyHeaders(t *testing.T) {
	var gotIP net.IP
	handler := RealIP(
		WithTrustedProxies([]string{"10.0.0.0/8"}),
		WithTrustedHeaders([]string{}), // Empty falls back to default
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP, _ = RealIPFromRequest(r)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.50")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if gotIP.String() != "203.0.113.50" {
		t.Errorf("got IP %v, want 203.0.113.50", gotIP)
	}
}

func TestRealIPFromContext_Nil(t *testing.T) {
	ip, ok := RealIPFromContext(context.Background())
	if ok || ip != nil {
		t.Errorf("expected nil/false for empty context, got %v/%v", ip, ok)
	}
}

func TestRealIPFromRequest_Nil(t *testing.T) {
	ip, ok := RealIPFromRequest(nil)
	if ok || ip != nil {
		t.Errorf("expected nil/false for nil request, got %v/%v", ip, ok)
	}
}

func TestWithRealIP(t *testing.T) {
	ip := net.ParseIP("203.0.113.50")
	ctx := WithRealIP(context.Background(), ip)

	gotIP, ok := RealIPFromContext(ctx)
	if !ok {
		t.Error("expected ok to be true")
	}
	if !gotIP.Equal(ip) {
		t.Errorf("got IP %v, want %v", gotIP, ip)
	}
}

func TestWithRealIP_NilIP(t *testing.T) {
	ctx := context.Background()
	newCtx := WithRealIP(ctx, nil)

	// Should return unchanged context.
	if newCtx != ctx {
		t.Error("expected same context when IP is nil")
	}
}

func TestRealIP_PanicOnNilHandler(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil handler")
		}
	}()
	RealIP()(nil)
}

func TestRealIP_InvalidCIDRIgnored(t *testing.T) {
	var gotIP net.IP
	handler := RealIP(
		WithTrustedProxies([]string{"invalid", "10.0.0.0/8", "also-invalid"}),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP, _ = RealIPFromRequest(r)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.50")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	// Valid CIDR should still work.
	if gotIP.String() != "203.0.113.50" {
		t.Errorf("got IP %v, want 203.0.113.50", gotIP)
	}
}

func TestParseIP(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"192.168.1.1", "192.168.1.1"},
		{"192.168.1.1:8080", "192.168.1.1"},
		{"[::1]:8080", "::1"},
		{"::1", "::1"},
		{"2001:db8::1", "2001:db8::1"},
		{"[2001:db8::1]:443", "2001:db8::1"},
		{"", ""},
		{"   ", ""},
		{"  192.168.1.1  ", "192.168.1.1"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseIP(tt.input)
			if tt.want == "" {
				if got != nil {
					t.Errorf("parseIP(%q) = %v, want nil", tt.input, got)
				}
			} else if got == nil || got.String() != tt.want {
				t.Errorf("parseIP(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractFromXFF(t *testing.T) {
	trusted := []*net.IPNet{
		mustParseCIDR("10.0.0.0/8"),
		mustParseCIDR("192.168.0.0/16"),
	}

	tests := []struct {
		name  string
		xff   string
		want  string
		isNil bool
	}{
		{
			name: "single untrusted IP",
			xff:  "203.0.113.50",
			want: "203.0.113.50",
		},
		{
			name: "chain ending with trusted",
			xff:  "203.0.113.50, 10.0.0.1",
			want: "203.0.113.50",
		},
		{
			name: "chain with multiple trusted",
			xff:  "203.0.113.50, 10.0.0.1, 192.168.1.1",
			want: "203.0.113.50",
		},
		{
			name:  "all trusted",
			xff:   "10.0.0.1, 192.168.1.1",
			isNil: true,
		},
		{
			name:  "empty",
			xff:   "",
			isNil: true,
		},
		{
			name: "with spaces",
			xff:  "  203.0.113.50  ,  10.0.0.1  ",
			want: "203.0.113.50",
		},
		{
			name:  "invalid IP in chain",
			xff:   "203.0.113.50, invalid, 10.0.0.1",
			isNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFromXFF(tt.xff, trusted, XFFInvalidStop)
			if tt.isNil {
				if got != nil {
					t.Errorf("extractFromXFF(%q) = %v, want nil", tt.xff, got)
				}
			} else if got == nil || got.String() != tt.want {
				t.Errorf("extractFromXFF(%q) = %v, want %v", tt.xff, got, tt.want)
			}
		})
	}
}

func TestRealIP_MultipleXFFHeaders(t *testing.T) {
	// HTTP allows multiple headers with the same name.
	// They should be treated as comma-joined per RFC 7230.
	var gotIP net.IP
	handler := RealIP(
		WithTrustedProxies([]string{"10.0.0.0/8"}),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP, _ = RealIPFromRequest(r)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	// Multiple XFF headers: should be joined as "203.0.113.50, 10.0.0.2"
	req.Header.Add("X-Forwarded-For", "203.0.113.50")
	req.Header.Add("X-Forwarded-For", "10.0.0.2")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	// Scanning right-to-left: 10.0.0.2 is trusted, so return 203.0.113.50.
	if gotIP.String() != "203.0.113.50" {
		t.Errorf("got IP %v, want 203.0.113.50", gotIP)
	}
}

func TestRealIP_MultipleXFFHeadersComplex(t *testing.T) {
	// More complex case: multiple headers with chains.
	var gotIP net.IP
	handler := RealIP(
		WithTrustedProxies([]string{"10.0.0.0/8", "192.168.0.0/16"}),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP, _ = RealIPFromRequest(r)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	// First header: client and first proxy
	req.Header.Add("X-Forwarded-For", "203.0.113.50, 192.168.1.1")
	// Second header: added by second proxy
	req.Header.Add("X-Forwarded-For", "10.0.0.2")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	// Joined: "203.0.113.50, 192.168.1.1, 10.0.0.2"
	// Scanning right-to-left: 10.0.0.2 trusted, 192.168.1.1 trusted, 203.0.113.50 untrusted.
	if gotIP.String() != "203.0.113.50" {
		t.Errorf("got IP %v, want 203.0.113.50", gotIP)
	}
}

func TestRealIP_IPv6InXFF(t *testing.T) {
	var gotIP net.IP
	handler := RealIP(
		WithTrustedProxies([]string{"10.0.0.0/8"}),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP, _ = RealIPFromRequest(r)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "2001:db8::1, 10.0.0.2")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if gotIP.String() != "2001:db8::1" {
		t.Errorf("got IP %v, want 2001:db8::1", gotIP)
	}
}

func TestRealIP_IPv4MappedIPv6(t *testing.T) {
	// On dual-stack servers, IPv4 clients may appear as ::ffff:x.x.x.x.
	// The middleware should still match against IPv4 CIDR rules.
	var gotIP net.IP
	handler := RealIP(
		WithTrustedProxies([]string{"10.0.0.0/8"}),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP, _ = RealIPFromRequest(r)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	// IPv4-mapped IPv6 address for 10.0.0.1
	req.RemoteAddr = "[::ffff:10.0.0.1]:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.50")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	// Should recognize ::ffff:10.0.0.1 as trusted (matches 10.0.0.0/8).
	if gotIP.String() != "203.0.113.50" {
		t.Errorf("got IP %v, want 203.0.113.50 (IPv4-mapped should match)", gotIP)
	}
}

func TestRealIP_IPv4MappedIPv6InXFF(t *testing.T) {
	// IPv4-mapped addresses in XFF should also be normalized.
	var gotIP net.IP
	handler := RealIP(
		WithTrustedProxies([]string{"10.0.0.0/8"}),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP, _ = RealIPFromRequest(r)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	// XFF contains IPv4-mapped IPv6 for a trusted proxy
	req.Header.Set("X-Forwarded-For", "203.0.113.50, ::ffff:10.0.0.2")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	// ::ffff:10.0.0.2 should be recognized as trusted.
	if gotIP.String() != "203.0.113.50" {
		t.Errorf("got IP %v, want 203.0.113.50", gotIP)
	}
}

func TestRealIP_CustomNonStandardHeader(t *testing.T) {
	// Test with a custom header like CF-Connecting-IP.
	var gotIP net.IP
	handler := RealIP(
		WithTrustedProxies([]string{"10.0.0.0/8"}),
		WithTrustedHeaders([]string{"CF-Connecting-IP", "X-Forwarded-For"}),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP, _ = RealIPFromRequest(r)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("CF-Connecting-IP", "203.0.113.99")
	req.Header.Set("X-Forwarded-For", "1.2.3.4")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	// CF-Connecting-IP takes priority.
	if gotIP.String() != "203.0.113.99" {
		t.Errorf("got IP %v, want 203.0.113.99", gotIP)
	}
}

func TestRealIP_NilOption(t *testing.T) {
	// nil options should be safely ignored.
	var gotIP net.IP
	handler := RealIP(
		nil,
		WithTrustedProxies([]string{"10.0.0.0/8"}),
		nil,
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP, _ = RealIPFromRequest(r)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.50")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if gotIP.String() != "203.0.113.50" {
		t.Errorf("got IP %v, want 203.0.113.50", gotIP)
	}
}

func TestRealIP_EmptyTrustedProxiesList(t *testing.T) {
	// Empty list should behave same as no trusted proxies.
	var gotIP net.IP
	handler := RealIP(
		WithTrustedProxies([]string{}),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP, _ = RealIPFromRequest(r)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.50")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	// Should use RemoteAddr, not XFF.
	if gotIP.String() != "10.0.0.1" {
		t.Errorf("got IP %v, want 10.0.0.1 (headers ignored)", gotIP)
	}
}

func TestRealIP_XRealIPNotTrustedChecked(t *testing.T) {
	// X-Real-IP is used directly without trusted proxy chain checking.
	// This is by design: X-Real-IP is typically set by the first proxy.
	var gotIP net.IP
	handler := RealIP(
		WithTrustedProxies([]string{"10.0.0.0/8"}),
		WithTrustedHeaders([]string{"X-Real-IP"}),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP, _ = RealIPFromRequest(r)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	// X-Real-IP contains a trusted IP (would be skipped if treated like XFF).
	req.Header.Set("X-Real-IP", "10.0.0.99")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	// X-Real-IP is used directly without chain checking.
	if gotIP.String() != "10.0.0.99" {
		t.Errorf("got IP %v, want 10.0.0.99", gotIP)
	}
}

func TestRealIP_MultipleXRealIPValuesAreRejected(t *testing.T) {
	// X-Real-IP (and other single-value headers) should be rejected if multiple values exist.
	// This avoids ambiguous/conflicting header semantics.
	var gotIP net.IP
	handler := RealIP(
		WithTrustedProxies([]string{"10.0.0.0/8"}),
		WithTrustedHeaders([]string{"X-Real-IP", "X-Forwarded-For"}),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP, _ = RealIPFromRequest(r)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	// Multiple X-Real-IP values: should be rejected, so we fall back to XFF.
	req.Header.Add("X-Real-IP", "2.2.2.2")
	req.Header.Add("X-Real-IP", "3.3.3.3")
	req.Header.Set("X-Forwarded-For", "203.0.113.50")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if gotIP.String() != "203.0.113.50" {
		t.Errorf("got IP %v, want 203.0.113.50 (fallback to XFF)", gotIP)
	}
}

func TestRealIP_XFFTokenWithPort(t *testing.T) {
	// Some proxies may include a port in XFF tokens (non-standard but seen in practice).
	var gotIP net.IP
	handler := RealIP(
		WithTrustedProxies([]string{"10.0.0.0/8"}),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP, _ = RealIPFromRequest(r)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.50:9999, 10.0.0.2")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if gotIP.String() != "203.0.113.50" {
		t.Errorf("got IP %v, want 203.0.113.50", gotIP)
	}
}

func TestRealIP_WhitespaceOnlyHeader(t *testing.T) {
	var gotIP net.IP
	handler := RealIP(
		WithTrustedProxies([]string{"10.0.0.0/8"}),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP, _ = RealIPFromRequest(r)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "   ")
	req.Header.Set("X-Real-IP", "203.0.113.50")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	// XFF is whitespace-only, falls back to X-Real-IP.
	if gotIP.String() != "203.0.113.50" {
		t.Errorf("got IP %v, want 203.0.113.50", gotIP)
	}
}

func TestRealIP_RemoteAddrUnparseable(t *testing.T) {
	// If RemoteAddr cannot be parsed, RealIP should not inject a nil value into context.
	// Downstream extraction should return (nil, false).
	var gotIP net.IP
	var gotOK bool
	handler := RealIP()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP, gotOK = RealIPFromRequest(r)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "not-an-ip"

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if gotOK || gotIP != nil {
		t.Errorf("expected nil/false for unparseable RemoteAddr, got %v/%v", gotIP, gotOK)
	}
}

func TestIsTrustedIP(t *testing.T) {
	trusted := []*net.IPNet{
		mustParseCIDR("10.0.0.0/8"),
		mustParseCIDR("192.168.0.0/16"),
		mustParseCIDR("::1/128"),
	}

	tests := []struct {
		ip   string
		want bool
	}{
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"192.168.1.1", true},
		{"192.168.255.255", true},
		{"::1", true},
		{"203.0.113.50", false},
		{"192.169.0.1", false},
		{"2001:db8::1", false},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			got := isTrustedIP(ip, trusted)
			if got != tt.want {
				t.Errorf("isTrustedIP(%q) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func mustParseCIDR(s string) *net.IPNet {
	_, ipNet, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return ipNet
}

func TestParseTrustedProxies(t *testing.T) {
	nets, err := ParseTrustedProxies([]string{"10.0.0.0/8", "  ", "invalid", "192.168.1.1"})
	if err == nil {
		t.Fatal("expected error for invalid entry")
	}
	if len(nets) != 2 {
		t.Fatalf("expected 2 parsed nets, got %d", len(nets))
	}
}

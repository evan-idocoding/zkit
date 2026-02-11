package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCORS_NoOriginHeader_IsNoop(t *testing.T) {
	var downstreamCalled bool
	h := CORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downstreamCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if !downstreamCalled {
		t.Fatalf("expected downstream called")
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("expected no CORS headers, got ACAO=%q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestCORS_Default_AllowsAnyOriginAndCredentials(t *testing.T) {
	var downstreamCalled bool
	h := CORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downstreamCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Set("Origin", "https://frontend.example")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if !downstreamCalled {
		t.Fatalf("expected downstream called")
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "https://frontend.example" {
		t.Fatalf("expected ACAO reflected, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
	if rr.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("expected ACAC=true, got %q", rr.Header().Get("Access-Control-Allow-Credentials"))
	}
	if rr.Header().Get("Access-Control-Expose-Headers") != DefaultRequestIDHeader {
		t.Fatalf("expected ACEH=%q, got %q", DefaultRequestIDHeader, rr.Header().Get("Access-Control-Expose-Headers"))
	}
	if !varyContains(rr.Header(), "Origin") {
		t.Fatalf("expected Vary contains Origin, got %q", rr.Header().Values("Vary"))
	}
}

func TestCORS_ExposeHeaders_DisableAndCustom(t *testing.T) {
	t.Run("disable", func(t *testing.T) {
		h := CORS(WithExposeHeaders(nil))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.Header.Set("Origin", "https://frontend.example")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		if rr.Header().Get("Access-Control-Expose-Headers") != "" {
			t.Fatalf("expected ACEH unset when disabled, got %q", rr.Header().Get("Access-Control-Expose-Headers"))
		}
	})

	t.Run("custom", func(t *testing.T) {
		h := CORS(WithExposeHeaders([]string{"X-Foo", " x-foo ", "X-Bar"}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.Header.Set("Origin", "https://frontend.example")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		if rr.Header().Get("Access-Control-Expose-Headers") != "X-Foo, X-Bar" {
			t.Fatalf("expected ACEH=%q, got %q", "X-Foo, X-Bar", rr.Header().Get("Access-Control-Expose-Headers"))
		}
	})
}

func TestCORS_ExposeHeadersAppend(t *testing.T) {
	t.Run("append to default", func(t *testing.T) {
		h := CORS(WithExposeHeadersAppend([]string{"X-Foo"}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.Header.Set("Origin", "https://frontend.example")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		if rr.Header().Get("Access-Control-Expose-Headers") != "X-Request-ID, X-Foo" {
			t.Fatalf("expected ACEH=%q, got %q", "X-Request-ID, X-Foo", rr.Header().Get("Access-Control-Expose-Headers"))
		}
	})

	t.Run("disable then append", func(t *testing.T) {
		h := CORS(
			WithExposeHeaders(nil),
			WithExposeHeadersAppend([]string{"X-Foo"}),
		)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.Header.Set("Origin", "https://frontend.example")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		if rr.Header().Get("Access-Control-Expose-Headers") != "X-Foo" {
			t.Fatalf("expected ACEH=%q, got %q", "X-Foo", rr.Header().Get("Access-Control-Expose-Headers"))
		}
	})
}

func TestCORS_WithAllowCredentialsFalse_DoesNotSetHeader(t *testing.T) {
	h := CORS(WithAllowCredentials(false))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Set("Origin", "https://frontend.example")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "https://frontend.example" {
		t.Fatalf("expected ACAO reflected, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
	if rr.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("expected no ACAC when disabled, got %q", rr.Header().Get("Access-Control-Allow-Credentials"))
	}
}

func TestCORS_WithAllowCredentialsFalse_Preflight_DoesNotSetHeader(t *testing.T) {
	h := CORS(WithAllowCredentials(false))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("downstream should not be called for allowed preflight")
	}))

	req := httptest.NewRequest(http.MethodOptions, "http://example.test/", nil)
	req.Header.Set("Origin", "https://frontend.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("expected no ACAC when disabled, got %q", rr.Header().Get("Access-Control-Allow-Credentials"))
	}
}

func TestCORS_WithAllowedOrigins_DisallowsNonMatchingOrigin(t *testing.T) {
	var downstreamCalled bool
	h := CORS(WithAllowedOrigins([]string{"https://a.example"}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downstreamCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Set("Origin", "https://b.example")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if !downstreamCalled {
		t.Fatalf("expected downstream called")
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("expected no ACAO for disallowed origin, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORS_WithAllowedOrigins_TrimsAndIgnoresBlanks(t *testing.T) {
	h := CORS(WithAllowedOrigins([]string{"  ", "https://a.example", "\t"}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Set("Origin", "https://a.example")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "https://a.example" {
		t.Fatalf("expected ACAO set after trimming allowlist, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORS_WithAllowedOrigins_AllBlanksMeansDenyAll(t *testing.T) {
	h := CORS(WithAllowedOrigins([]string{"  ", "\t"}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Set("Origin", "https://any.example")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("expected no ACAO when configured allowlist has no valid entries, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORS_WithAllowedOrigins_AllowsMatchingOrigin(t *testing.T) {
	h := CORS(WithAllowedOrigins([]string{"https://a.example"}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Set("Origin", "https://a.example")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "https://a.example" {
		t.Fatalf("expected ACAO set, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORS_WithAllowedOrigins_DomainAndWildcardMatching(t *testing.T) {
	type tc struct {
		name     string
		patterns []string
		origin   string
		allowed  bool
	}
	tests := []tc{
		{
			name:     "example.com matches itself and any subdomain (ignore scheme/port)",
			patterns: []string{"example.com"},
			origin:   "https://example.com",
			allowed:  true,
		},
		{
			name:     "example.com matches subdomain",
			patterns: []string{"example.com"},
			origin:   "https://a.example.com",
			allowed:  true,
		},
		{
			name:     "example.com matches deeper subdomain",
			patterns: []string{"example.com"},
			origin:   "https://a.b.example.com",
			allowed:  true,
		},
		{
			name:     "example.com ignores scheme and port",
			patterns: []string{"https://example.com:8443"},
			origin:   "http://example.com:8080",
			allowed:  true,
		},
		{
			name:     "example.com does not match evil-example.com",
			patterns: []string{"example.com"},
			origin:   "https://evil-example.com",
			allowed:  false,
		},
		{
			name:     "example.com does not match example.com.evil.com",
			patterns: []string{"example.com"},
			origin:   "https://example.com.evil.com",
			allowed:  false,
		},
		{
			name:     "*.example.com matches subdomain",
			patterns: []string{"*.example.com"},
			origin:   "https://a.example.com",
			allowed:  true,
		},
		{
			name:     "*.example.com does not match apex",
			patterns: []string{"*.example.com"},
			origin:   "https://example.com",
			allowed:  false,
		},
		{
			name:     "*.a.example.com matches deeper only",
			patterns: []string{"*.a.example.com"},
			origin:   "https://b.a.example.com",
			allowed:  true,
		},
		{
			name:     "*.a.example.com does not match a.example.com itself",
			patterns: []string{"*.a.example.com"},
			origin:   "https://a.example.com",
			allowed:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := CORS(WithAllowedOrigins(tt.patterns))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
			req.Header.Set("Origin", tt.origin)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)

			if tt.allowed {
				if rr.Header().Get("Access-Control-Allow-Origin") != tt.origin {
					t.Fatalf("expected ACAO reflected, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
				}
				return
			}
			if rr.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatalf("expected no ACAO for disallowed origin, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
			}
		})
	}
}

func TestCORS_WithAllowNullOrigin(t *testing.T) {
	t.Run("allowlist without null enabled denies null", func(t *testing.T) {
		h := CORS(WithAllowedOrigins([]string{"example.com"}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.Header.Set("Origin", "null")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("expected no ACAO for null when not enabled, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
		}
	})

	t.Run("allowlist with null enabled allows null", func(t *testing.T) {
		h := CORS(
			WithAllowedOrigins([]string{"example.com"}),
			WithAllowNullOrigin(true),
		)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.Header.Set("Origin", "null")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Header().Get("Access-Control-Allow-Origin") != "null" {
			t.Fatalf("expected ACAO=null, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
		}
	})

	t.Run("deny-all patterns still deny null even if enabled", func(t *testing.T) {
		h := CORS(
			WithAllowedOrigins([]string{"   "}), // parses to zero patterns -> deny all (fail-closed)
			WithAllowNullOrigin(true),
		)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.Header.Set("Origin", "null")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("expected no ACAO for null when deny-all, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
		}
	})
}

func TestCORS_Preflight_ShortCircuitsAndReflectsMethodAndHeaders(t *testing.T) {
	var downstreamCalled int32
	h := CORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&downstreamCalled, 1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "http://example.test/", nil)
	req.Header.Set("Origin", "https://frontend.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Add("Access-Control-Request-Headers", "X-Token")
	req.Header.Add("Access-Control-Request-Headers", "X-Trace")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if atomic.LoadInt32(&downstreamCalled) != 0 {
		t.Fatalf("expected downstream not called for allowed preflight")
	}
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "https://frontend.example" {
		t.Fatalf("expected ACAO reflected, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
	if rr.Header().Get("Access-Control-Allow-Methods") != "POST" {
		t.Fatalf("expected ACAM=POST, got %q", rr.Header().Get("Access-Control-Allow-Methods"))
	}
	if rr.Header().Get("Access-Control-Allow-Headers") != "X-Token, X-Trace" {
		t.Fatalf("expected ACAH joined, got %q", rr.Header().Get("Access-Control-Allow-Headers"))
	}
	if rr.Header().Get("Access-Control-Expose-Headers") != "" {
		t.Fatalf("expected ACEH unset for preflight, got %q", rr.Header().Get("Access-Control-Expose-Headers"))
	}
	if rr.Header().Get("Access-Control-Max-Age") != "600" {
		t.Fatalf("expected ACMA=600 (10m default), got %q", rr.Header().Get("Access-Control-Max-Age"))
	}
	if !varyContains(rr.Header(), "Origin") ||
		!varyContains(rr.Header(), "Access-Control-Request-Method") ||
		!varyContains(rr.Header(), "Access-Control-Request-Headers") {
		t.Fatalf("expected Vary contains origin+acrm+acrh, got %q", rr.Header().Values("Vary"))
	}
}

func TestCORS_MaxAge_CustomAndDisable(t *testing.T) {
	t.Run("custom", func(t *testing.T) {
		h := CORS(WithMaxAge(2 * time.Minute))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatalf("downstream should not be called for allowed preflight")
		}))

		req := httptest.NewRequest(http.MethodOptions, "http://example.test/", nil)
		req.Header.Set("Origin", "https://frontend.example")
		req.Header.Set("Access-Control-Request-Method", "POST")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		if rr.Header().Get("Access-Control-Max-Age") != "120" {
			t.Fatalf("expected ACMA=120, got %q", rr.Header().Get("Access-Control-Max-Age"))
		}
	})

	t.Run("disable", func(t *testing.T) {
		h := CORS(WithMaxAge(0))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatalf("downstream should not be called for allowed preflight")
		}))

		req := httptest.NewRequest(http.MethodOptions, "http://example.test/", nil)
		req.Header.Set("Origin", "https://frontend.example")
		req.Header.Set("Access-Control-Request-Method", "POST")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		if rr.Header().Get("Access-Control-Max-Age") != "" {
			t.Fatalf("expected ACMA unset when disabled, got %q", rr.Header().Get("Access-Control-Max-Age"))
		}
	})
}

func TestCORS_PreflightStatus(t *testing.T) {
	h := CORS(WithPreflightStatus(http.StatusOK))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("downstream should not be called for allowed preflight")
	}))

	req := httptest.NewRequest(http.MethodOptions, "http://example.test/", nil)
	req.Header.Set("Origin", "https://frontend.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestCORS_Preflight_DisallowedOrigin_DoesNotShortCircuit(t *testing.T) {
	var downstreamCalled int32
	h := CORS(WithAllowedOrigins([]string{"https://a.example"}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&downstreamCalled, 1)
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest(http.MethodOptions, "http://example.test/", nil)
	req.Header.Set("Origin", "https://b.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if atomic.LoadInt32(&downstreamCalled) != 1 {
		t.Fatalf("expected downstream called for disallowed preflight")
	}
	if rr.Code != http.StatusTeapot {
		t.Fatalf("expected downstream status %d, got %d", http.StatusTeapot, rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("expected no ACAO for disallowed origin, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORS_OptionsWithoutACRM_IsNotPreflight_PassesThroughWithCORSHeaders(t *testing.T) {
	var downstreamCalled int32
	h := CORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&downstreamCalled, 1)
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest(http.MethodOptions, "http://example.test/", nil)
	req.Header.Set("Origin", "https://frontend.example")
	// No Access-Control-Request-Method -> not a preflight per our definition.
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if atomic.LoadInt32(&downstreamCalled) != 1 {
		t.Fatalf("expected downstream called for non-preflight OPTIONS")
	}
	if rr.Code != http.StatusTeapot {
		t.Fatalf("expected downstream status %d, got %d", http.StatusTeapot, rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "https://frontend.example" {
		t.Fatalf("expected ACAO reflected, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
	if rr.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("expected ACAC=true, got %q", rr.Header().Get("Access-Control-Allow-Credentials"))
	}
	if rr.Header().Get("Access-Control-Max-Age") != "" {
		t.Fatalf("expected ACMA unset for non-preflight, got %q", rr.Header().Get("Access-Control-Max-Age"))
	}
}

func TestCORS_AllowedMethods_RestrictsPreflightAndNonPreflight(t *testing.T) {
	t.Run("non-preflight method not allowed skips CORS", func(t *testing.T) {
		h := CORS(WithAllowedMethods([]string{"GET"}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodPost, "http://example.test/", nil)
		req.Header.Set("Origin", "https://frontend.example")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		if rr.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("expected no ACAO when method disallowed, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
		}
	})

	t.Run("preflight request method not allowed does not short-circuit", func(t *testing.T) {
		var downstreamCalled int32
		h := CORS(WithAllowedMethods([]string{"GET"}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&downstreamCalled, 1)
			w.WriteHeader(http.StatusTeapot)
		}))

		req := httptest.NewRequest(http.MethodOptions, "http://example.test/", nil)
		req.Header.Set("Origin", "https://frontend.example")
		req.Header.Set("Access-Control-Request-Method", "POST")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		if atomic.LoadInt32(&downstreamCalled) != 1 {
			t.Fatalf("expected downstream called when preflight method disallowed")
		}
		if rr.Code != http.StatusTeapot {
			t.Fatalf("expected downstream status %d, got %d", http.StatusTeapot, rr.Code)
		}
		if rr.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("expected no ACAO when preflight method disallowed, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
		}
	})

	t.Run("invalid config denies all methods (fail-closed)", func(t *testing.T) {
		h := CORS(WithAllowedMethods([]string{"   "}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.Header.Set("Origin", "https://frontend.example")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		if rr.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("expected no ACAO when allowedMethods invalid (deny all), got %q", rr.Header().Get("Access-Control-Allow-Origin"))
		}
	})
}

func TestCORS_AllowedHeaders_RestrictsPreflight(t *testing.T) {
	t.Run("allowed", func(t *testing.T) {
		h := CORS(WithAllowedHeaders([]string{"x-token"}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatalf("downstream should not be called for allowed preflight")
		}))

		req := httptest.NewRequest(http.MethodOptions, "http://example.test/", nil)
		req.Header.Set("Origin", "https://frontend.example")
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "X-Token")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		if rr.Code != http.StatusNoContent {
			t.Fatalf("expected status %d, got %d", http.StatusNoContent, rr.Code)
		}
		if rr.Header().Get("Access-Control-Allow-Origin") != "https://frontend.example" {
			t.Fatalf("expected ACAO reflected, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
		}
		if rr.Header().Get("Access-Control-Allow-Headers") != "X-Token" {
			t.Fatalf("expected ACAH reflected, got %q", rr.Header().Get("Access-Control-Allow-Headers"))
		}
	})

	t.Run("disallowed does not short-circuit", func(t *testing.T) {
		var downstreamCalled int32
		h := CORS(WithAllowedHeaders([]string{"x-token"}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&downstreamCalled, 1)
			w.WriteHeader(http.StatusTeapot)
		}))

		req := httptest.NewRequest(http.MethodOptions, "http://example.test/", nil)
		req.Header.Set("Origin", "https://frontend.example")
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "X-Token, X-Trace")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		if atomic.LoadInt32(&downstreamCalled) != 1 {
			t.Fatalf("expected downstream called when requested headers disallowed")
		}
		if rr.Code != http.StatusTeapot {
			t.Fatalf("expected downstream status %d, got %d", http.StatusTeapot, rr.Code)
		}
		if rr.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("expected no ACAO when requested headers disallowed, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
		}
	})
}

func TestCORS_EnabledFuncFalse_SkipsEvenForPreflight(t *testing.T) {
	var downstreamCalled int32
	h := CORS(WithEnabledFunc(func(r *http.Request) bool { return false }))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&downstreamCalled, 1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "http://example.test/", nil)
	req.Header.Set("Origin", "https://frontend.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if atomic.LoadInt32(&downstreamCalled) != 1 {
		t.Fatalf("expected downstream called when disabled")
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("expected no CORS headers when disabled, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORS_MatchFuncFalse_Skips(t *testing.T) {
	var downstreamCalled int32
	h := CORS(WithMatchFunc(func(r *http.Request) bool { return false }))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&downstreamCalled, 1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Set("Origin", "https://frontend.example")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if atomic.LoadInt32(&downstreamCalled) != 1 {
		t.Fatalf("expected downstream called when match=false")
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("expected no CORS headers when match=false, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORS_MultipleOriginValues_TreatedAsInvalidAndSkipped(t *testing.T) {
	var downstreamCalled int32
	h := CORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&downstreamCalled, 1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "http://example.test/", nil)
	req.Header.Add("Origin", "https://a.example")
	req.Header.Add("Origin", "https://b.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if atomic.LoadInt32(&downstreamCalled) != 1 {
		t.Fatalf("expected downstream called when Origin is ambiguous")
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("expected no ACAO when Origin is ambiguous, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORS_MultipleOriginValues_NonPreflight_TreatedAsInvalidAndSkipped(t *testing.T) {
	var downstreamCalled int32
	h := CORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&downstreamCalled, 1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Add("Origin", "https://a.example")
	req.Header.Add("Origin", "https://b.example")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if atomic.LoadInt32(&downstreamCalled) != 1 {
		t.Fatalf("expected downstream called when Origin is ambiguous")
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("expected no ACAO when Origin is ambiguous, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestAddVary_NoDuplicates(t *testing.T) {
	h := http.Header{}
	addVary(h, "Origin")
	addVary(h, "Origin")
	if got := h.Values("Vary"); len(got) != 1 || strings.TrimSpace(got[0]) != "Origin" {
		t.Fatalf("expected single Vary=Origin, got %q", got)
	}
}

func TestAddVary_RespectsWildcard(t *testing.T) {
	h := http.Header{}
	h.Set("Vary", "*")
	addVary(h, "Origin")
	if got := h.Values("Vary"); len(got) != 1 || strings.TrimSpace(got[0]) != "*" {
		t.Fatalf("expected Vary stays '*', got %q", got)
	}
}

func TestAddVary_DetectsExistingTokensInCommaList(t *testing.T) {
	h := http.Header{}
	h.Set("Vary", "Accept-Encoding, Origin")
	addVary(h, "Origin")
	addVary(h, "Access-Control-Request-Method")
	if !varyContains(h, "Origin") || !varyContains(h, "Accept-Encoding") || !varyContains(h, "Access-Control-Request-Method") {
		t.Fatalf("expected vary tokens present, got %q", h.Values("Vary"))
	}
	// Origin should not be duplicated.
	originCount := 0
	for _, raw := range h.Values("Vary") {
		for _, part := range strings.Split(raw, ",") {
			if strings.EqualFold(strings.TrimSpace(part), "Origin") {
				originCount++
			}
		}
	}
	if originCount != 1 {
		t.Fatalf("expected Origin token once, got %d in %q", originCount, h.Values("Vary"))
	}
}

func varyContains(h http.Header, token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return false
	}
	for _, raw := range h.Values("Vary") {
		for _, part := range strings.Split(raw, ",") {
			if strings.ToLower(strings.TrimSpace(part)) == token {
				return true
			}
		}
	}
	return false
}

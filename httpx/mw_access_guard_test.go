package httpx

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestAccessGuard_PanicsWhenNoChecksConfigured(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic")
		}
	}()
	_ = AccessGuard()
}

func TestAccessGuard_TokenOnly_WithTokens(t *testing.T) {
	h := Chain(AccessGuard(
		WithTokenHeader("X-Access-Token"),
		WithTokens([]string{"p1"}),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("allows when token matches", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.Header.Set("X-Access-Token", "p1")
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("denies when header missing", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
		}
	})

	t.Run("denies when multiple header values", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.Header.Add("X-Access-Token", "p1")
		req.Header.Add("X-Access-Token", "p1")
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
		}
	})

	t.Run("denies when token set empty (fail-closed)", func(t *testing.T) {
		h2 := Chain(AccessGuard(WithTokens([]string{}))).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.Header.Set(DefaultAccessGuardTokenHeader, "anything")
		h2.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
		}
	})

	t.Run("denies when tokens are all blank (fail-closed)", func(t *testing.T) {
		h2 := Chain(AccessGuard(WithTokens([]string{" ", "\t"}))).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.Header.Set(DefaultAccessGuardTokenHeader, "p1")
		h2.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
		}
	})
}

func TestAccessGuard_IPOnly_RemoteAddrFallback(t *testing.T) {
	h := Chain(AccessGuard(
		WithIPAllowList([]string{"10.0.0.0/8", "192.168.1.1"}),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("allows matching CIDR", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.RemoteAddr = "10.1.2.3:12345"
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("allows matching single IP", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("denies non-matching", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.RemoteAddr = "203.0.113.50:12345"
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
		}
	})
}

func TestAccessGuard_IPAllowList_AllInvalid_DeniesAll(t *testing.T) {
	h := Chain(AccessGuard(
		WithIPAllowList([]string{"not-an-ip", "10.0.0.0/33", "  "}),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.RemoteAddr = "10.1.2.3:12345"
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
	}
}

func TestAccessGuard_IPAllowList_PartialInvalid_StillWorks(t *testing.T) {
	h := Chain(AccessGuard(
		WithIPAllowList([]string{"not-an-ip", "10.0.0.0/8"}),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.RemoteAddr = "10.1.2.3:12345"
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestAccessGuard_IPAllowList_NilOrEmpty_DeniesAll(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   []string
	}{
		{name: "nil", in: nil},
		{name: "empty", in: []string{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := Chain(AccessGuard(
				WithIPAllowList(tt.in),
			)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
			req.RemoteAddr = "10.1.2.3:12345"
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusForbidden {
				t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
			}
		})
	}
}

func TestAccessGuard_IPOnly_IPv6(t *testing.T) {
	h := Chain(AccessGuard(
		WithIPAllowList([]string{"fd00::/8", "2001:db8::1"}),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("allows matching IPv6 CIDR", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.RemoteAddr = "[fd00::1]:12345"
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("allows matching IPv6 single IP", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.RemoteAddr = "[2001:db8::1]:12345"
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("denies non-matching IPv6", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.RemoteAddr = "[2001:db8::2]:12345"
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
		}
	})
}

func TestAccessGuard_IPOnly_IPv4MappedIPv6_MatchesIPv4CIDR(t *testing.T) {
	h := Chain(AccessGuard(
		WithIPAllowList([]string{"10.0.0.0/8"}),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	// IPv4-mapped IPv6 representation of 10.1.2.3.
	req.RemoteAddr = "[::ffff:10.1.2.3]:12345"
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestAccessGuard_IPOnly_RemoteAddrUnparseable_Denies(t *testing.T) {
	h := Chain(AccessGuard(
		WithIPAllowList([]string{"10.0.0.0/8"}),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.RemoteAddr = "not-an-addr"
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
	}
}

func TestAccessGuard_TokenAndIP_AND(t *testing.T) {
	h := Chain(AccessGuard(
		WithTokens([]string{"p1"}),
		WithIPAllowList([]string{"10.0.0.0/8"}),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("allows when both match", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.RemoteAddr = "10.1.2.3:12345"
		req.Header.Set(DefaultAccessGuardTokenHeader, "p1")
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("denies when token wrong", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.RemoteAddr = "10.1.2.3:12345"
		req.Header.Set(DefaultAccessGuardTokenHeader, "nope")
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
		}
	})

	t.Run("denies when IP not allowed", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.RemoteAddr = "203.0.113.50:12345"
		req.Header.Set(DefaultAccessGuardTokenHeader, "p1")
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
		}
	})
}

func TestAccessGuard_TokenAndIP_OR(t *testing.T) {
	h := Chain(AccessGuard(
		WithTokens([]string{"p1"}),
		WithIPAllowList([]string{"10.0.0.0/8"}),
		WithOr(),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("allows when token ok, ip not ok", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.RemoteAddr = "203.0.113.50:12345"
		req.Header.Set(DefaultAccessGuardTokenHeader, "p1")
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("allows when ip ok, token not ok", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.RemoteAddr = "10.1.2.3:12345"
		req.Header.Set(DefaultAccessGuardTokenHeader, "nope")
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("denies when both not ok", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.RemoteAddr = "203.0.113.50:12345"
		req.Header.Set(DefaultAccessGuardTokenHeader, "nope")
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
		}
	})
}

func TestAccessGuard_UsesRealIPWhenPresent(t *testing.T) {
	// If AccessGuard used RemoteAddr directly, this request would be denied (RemoteAddr is 10.0.0.1).
	// RealIP should extract 203.0.113.50 from XFF and AccessGuard should allow it.
	h := Chain(
		RealIP(WithTrustedProxies([]string{"10.0.0.0/8"})),
		AccessGuard(WithIPAllowList([]string{"203.0.113.0/24"})),
	).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.50")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestAccessGuard_DenyStatus(t *testing.T) {
	h := Chain(AccessGuard(
		WithTokens([]string{"p1"}),
		WithDenyStatus(http.StatusUnauthorized),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Set(DefaultAccessGuardTokenHeader, "nope")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}

func TestAccessGuard_WithTokenSet_HotUpdate(t *testing.T) {
	set := NewAtomicTokenSet()
	set.Update([]string{"a"})

	h := Chain(AccessGuard(
		WithTokenSet(set),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	{
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.Header.Set(DefaultAccessGuardTokenHeader, "a")
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	}

	set.Update([]string{"b"})

	{
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.Header.Set(DefaultAccessGuardTokenHeader, "a")
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
		}
	}
	{
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.Header.Set(DefaultAccessGuardTokenHeader, "b")
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	}
}

func TestAccessGuard_WithCheck(t *testing.T) {
	h := Chain(AccessGuard(
		WithCheck(func(r *http.Request) bool {
			return r != nil && r.Method == http.MethodGet
		}),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}

	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "http://example.test/", nil)
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d", http.StatusForbidden, rr2.Code)
	}
}

func TestAccessGuard_WithCheck_ConflictsPanics(t *testing.T) {
	assertPanicsAccessGuard(t, func() {
		_ = AccessGuard(WithCheck(func(*http.Request) bool { return true }), WithTokens([]string{"x"}))
	})
	assertPanicsAccessGuard(t, func() {
		_ = AccessGuard(WithTokens([]string{"x"}), WithCheck(func(*http.Request) bool { return true }))
	})
	assertPanicsAccessGuard(t, func() {
		_ = AccessGuard(WithCheck(func(*http.Request) bool { return true }), WithIPAllowList([]string{"10.0.0.0/8"}))
	})
	assertPanicsAccessGuard(t, func() {
		_ = AccessGuard(WithCheck(func(*http.Request) bool { return true }), WithOr())
	})
	assertPanicsAccessGuard(t, func() {
		_ = AccessGuard(WithOr(), WithCheck(func(*http.Request) bool { return true }))
	})
}

func TestAccessGuard_OnDeny(t *testing.T) {
	var called int32
	var gotReason DenyReason
	h := Chain(AccessGuard(
		WithTokens([]string{"p1"}),
		WithOnDeny(func(r *http.Request, reason DenyReason) {
			atomic.AddInt32(&called, 1)
			gotReason = reason
		}),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Deny: missing token header.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
	}
	if atomic.LoadInt32(&called) != 1 {
		t.Fatalf("expected OnDeny called once, got %d", atomic.LoadInt32(&called))
	}
	if gotReason != DenyReasonTokenMissing {
		t.Fatalf("expected reason %q, got %q", DenyReasonTokenMissing, gotReason)
	}

	// Allow: should not call OnDeny.
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req2.Header.Set(DefaultAccessGuardTokenHeader, "p1")
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr2.Code)
	}
	if atomic.LoadInt32(&called) != 1 {
		t.Fatalf("expected OnDeny not called on allow, got %d", atomic.LoadInt32(&called))
	}
}

func TestAccessGuard_OnDenyPanicIsSwallowed(t *testing.T) {
	h := Chain(AccessGuard(
		WithTokens([]string{"p1"}),
		WithOnDeny(func(r *http.Request, reason DenyReason) {
			panic("boom")
		}),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("downstream should not be called")
	}))

	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("expected panic swallowed, got %v", p)
		}
	}()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
	}
}

func TestAccessGuard_PanicsOnNilNext(t *testing.T) {
	mw := AccessGuard(WithTokens([]string{"x"}))
	assertPanicsAccessGuard(t, func() { _ = mw(nil) })
}

func TestAccessGuard_WithTokenSetNilPanics(t *testing.T) {
	assertPanicsAccessGuard(t, func() {
		_ = AccessGuard(WithTokenSet(nil))
	})
}

func TestAccessGuard_WithIPAllowSetNilPanics(t *testing.T) {
	assertPanicsAccessGuard(t, func() {
		_ = AccessGuard(WithIPAllowSet(nil))
	})
}

func TestAccessGuard_TokenHeaderBlank_FallsBackToDefault(t *testing.T) {
	h := Chain(AccessGuard(
		WithTokenHeader("   "),
		WithTokens([]string{"p1"}),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Set(DefaultAccessGuardTokenHeader, "p1")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestAccessGuard_NilOptionsAreIgnored(t *testing.T) {
	t.Run("WithTokenCheck(nil) is ignored", func(t *testing.T) {
		// TokenCheck(nil) should not enable token validation and should not conflict.
		h := Chain(AccessGuard(
			WithTokenCheck(nil),
			WithIPAllowList([]string{"10.0.0.0/8"}),
		)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.RemoteAddr = "10.1.2.3:12345"
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("WithCheck(nil) is ignored", func(t *testing.T) {
		h := Chain(AccessGuard(
			WithTokens([]string{"p1"}),
			WithCheck(nil),
		)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.Header.Set(DefaultAccessGuardTokenHeader, "p1")
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("WithOnDeny(nil) is ignored", func(t *testing.T) {
		h := Chain(AccessGuard(
			WithTokens([]string{"p1"}),
			WithOnDeny(nil),
		)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.Header.Set(DefaultAccessGuardTokenHeader, "p1")
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})

	t.Run("WithIPResolver(nil) is ignored", func(t *testing.T) {
		h := Chain(AccessGuard(
			WithIPAllowList([]string{"10.0.0.0/8"}),
			WithIPResolver(nil),
		)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		req.RemoteAddr = "10.1.2.3:12345"
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
		}
	})
}

func assertPanicsAccessGuard(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic")
		}
	}()
	fn()
}

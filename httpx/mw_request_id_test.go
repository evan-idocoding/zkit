package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestID_UsesIncomingWhenValid(t *testing.T) {
	h := Chain(RequestID()).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := RequestIDFromRequest(r)
		if !ok {
			t.Fatalf("expected request id in context")
		}
		if got != "abc_123" {
			t.Fatalf("expected request id %q, got %q", "abc_123", got)
		}
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Set(DefaultRequestIDHeader, "abc_123")
	h.ServeHTTP(rr, req)

	if rr.Header().Get(DefaultRequestIDHeader) != "abc_123" {
		t.Fatalf("expected response header %q=%q, got %q", DefaultRequestIDHeader, "abc_123", rr.Header().Get(DefaultRequestIDHeader))
	}
}

func TestRequestID_GeneratesWhenMissing(t *testing.T) {
	h := Chain(RequestID(WithGenerator(func() (string, error) { return "gen1", nil }))).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := RequestIDFromRequest(r)
		if !ok || got != "gen1" {
			t.Fatalf("expected request id %q, got %q (ok=%v)", "gen1", got, ok)
		}
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	h.ServeHTTP(rr, req)

	if rr.Header().Get(DefaultRequestIDHeader) != "gen1" {
		t.Fatalf("expected response header %q=%q, got %q", DefaultRequestIDHeader, "gen1", rr.Header().Get(DefaultRequestIDHeader))
	}
}

func TestRequestID_InvalidIncoming_IsIgnored(t *testing.T) {
	h := Chain(RequestID(
		WithGenerator(func() (string, error) { return "gen2", nil }),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := RequestIDFromRequest(r)
		if got != "gen2" {
			t.Fatalf("expected generated id %q, got %q", "gen2", got)
		}
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Set(DefaultRequestIDHeader, "bad value") // contains whitespace -> invalid
	h.ServeHTTP(rr, req)

	if rr.Header().Get(DefaultRequestIDHeader) != "gen2" {
		t.Fatalf("expected response header %q=%q, got %q", DefaultRequestIDHeader, "gen2", rr.Header().Get(DefaultRequestIDHeader))
	}
}

func TestRequestID_IncomingHeaders_OrderAndConflict(t *testing.T) {
	h := Chain(RequestID(
		WithIncomingHeaders([]string{"X-Correlation-ID", DefaultRequestIDHeader}),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := RequestIDFromRequest(r)
		if got != "corr" {
			t.Fatalf("expected id %q, got %q", "corr", got)
		}
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Set(DefaultRequestIDHeader, "req")
	req.Header.Set("X-Correlation-ID", "corr")
	h.ServeHTTP(rr, req)

	// Outgoing is always DefaultRequestIDHeader.
	if rr.Header().Get(DefaultRequestIDHeader) != "corr" {
		t.Fatalf("expected response header %q=%q, got %q", DefaultRequestIDHeader, "corr", rr.Header().Get(DefaultRequestIDHeader))
	}
}

func TestRequestID_TrustIncomingFalse_AlwaysGenerates(t *testing.T) {
	h := Chain(RequestID(
		WithTrustIncoming(false),
		WithGenerator(func() (string, error) { return "gen3", nil }),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := RequestIDFromRequest(r)
		if got != "gen3" {
			t.Fatalf("expected id %q, got %q", "gen3", got)
		}
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Set(DefaultRequestIDHeader, "incoming")
	h.ServeHTTP(rr, req)

	if rr.Header().Get(DefaultRequestIDHeader) != "gen3" {
		t.Fatalf("expected response header %q=%q, got %q", DefaultRequestIDHeader, "gen3", rr.Header().Get(DefaultRequestIDHeader))
	}
}

func TestRequestID_SetResponseHeaderFalse_DoesNotWriteHeader(t *testing.T) {
	h := Chain(RequestID(
		WithSetResponseHeader(false),
		WithGenerator(func() (string, error) { return "gen4", nil }),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := RequestIDFromRequest(r)
		if got != "gen4" {
			t.Fatalf("expected id %q, got %q", "gen4", got)
		}
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	h.ServeHTTP(rr, req)

	if rr.Header().Get(DefaultRequestIDHeader) != "" {
		t.Fatalf("expected response header %q to be empty, got %q", DefaultRequestIDHeader, rr.Header().Get(DefaultRequestIDHeader))
	}
}

func TestRequestID_MaxLen(t *testing.T) {
	h := Chain(RequestID(
		WithMaxLen(3),
		WithGenerator(func() (string, error) { return "gen5", nil }),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := RequestIDFromRequest(r)
		if got != "gen5" {
			t.Fatalf("expected id %q, got %q", "gen5", got)
		}
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Set(DefaultRequestIDHeader, "toolong")
	h.ServeHTTP(rr, req)
}

func TestRequestID_MultiValueHeader_IsRejected(t *testing.T) {
	h := Chain(RequestID(
		WithGenerator(func() (string, error) { return "genMV", nil }),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := RequestIDFromRequest(r)
		if got != "genMV" {
			t.Fatalf("expected id %q, got %q", "genMV", got)
		}
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Add(DefaultRequestIDHeader, "a")
	req.Header.Add(DefaultRequestIDHeader, "b")
	// Repeated headers produce multiple values; RequestID rejects len(values)!=1 and falls back to generation.
	h.ServeHTTP(rr, req)

	if rr.Header().Get(DefaultRequestIDHeader) != "genMV" {
		t.Fatalf("expected response header %q=%q, got %q", DefaultRequestIDHeader, "genMV", rr.Header().Get(DefaultRequestIDHeader))
	}
}

func TestRequestID_WithIncomingHeaders_TrimsAndIgnoresEmpty(t *testing.T) {
	h := Chain(RequestID(
		WithIncomingHeaders([]string{"", "   ", "  X-Correlation-ID  ", "   "}),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := RequestIDFromRequest(r)
		if got != "corrTrim" {
			t.Fatalf("expected id %q, got %q", "corrTrim", got)
		}
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Set("X-Correlation-ID", "corrTrim")
	h.ServeHTTP(rr, req)
}

func TestRequestID_WithIncomingHeaders_AllEmpty_FallsBackToDefaultHeader(t *testing.T) {
	h := Chain(RequestID(
		WithIncomingHeaders([]string{"", "   "}),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := RequestIDFromRequest(r)
		if got != "fromDefault" {
			t.Fatalf("expected id %q, got %q", "fromDefault", got)
		}
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Set(DefaultRequestIDHeader, "fromDefault")
	h.ServeHTTP(rr, req)
}

func TestRequestID_GeneratorError_FallsBackToInternal(t *testing.T) {
	h := Chain(RequestID(
		WithGenerator(func() (string, error) { return "", errors.New("boom") }),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := RequestIDFromRequest(r)
		if !ok || got == "" {
			t.Fatalf("expected non-empty id")
		}
		// Should not equal the user generator output (empty).
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	h.ServeHTTP(rr, req)

	if rr.Header().Get(DefaultRequestIDHeader) == "" {
		t.Fatalf("expected response header to be set")
	}
}

func TestRequestID_GeneratorReturnsInvalid_FallsBackToInternal(t *testing.T) {
	h := Chain(RequestID(
		WithGenerator(func() (string, error) { return "bad value", nil }),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := RequestIDFromRequest(r)
		if !ok || got == "" {
			t.Fatalf("expected non-empty id")
		}
		if got == "bad value" {
			t.Fatalf("expected invalid generator output to be rejected")
		}
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	h.ServeHTTP(rr, req)
}

func TestRequestID_WithMaxLen_AffectsIncomingOnly_NotGenerated(t *testing.T) {
	h := Chain(RequestID(
		WithMaxLen(3),
		WithGenerator(func() (string, error) { return strings.Repeat("a", 64), nil }),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := RequestIDFromRequest(r)
		if got != strings.Repeat("a", 64) {
			t.Fatalf("expected generated id to be accepted (incoming maxLen should not apply), got %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	// Incoming should be rejected due to maxLen=3, forcing generation.
	req.Header.Set(DefaultRequestIDHeader, "toolong")
	h.ServeHTTP(rr, req)
}

func TestRequestID_WithValidator_AffectsIncomingOnly_NotGenerated(t *testing.T) {
	h := Chain(RequestID(
		WithValidator(func(string) bool { return false }), // reject all incoming
		WithGenerator(func() (string, error) { return "genOK", nil }),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ := RequestIDFromRequest(r)
		if got != "genOK" {
			t.Fatalf("expected generated id %q, got %q", "genOK", got)
		}
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	req.Header.Set(DefaultRequestIDHeader, "abc_123")
	h.ServeHTTP(rr, req)
}

func TestRequestID_ResponseHeaderIsSetBeforeNextHandler(t *testing.T) {
	h := Chain(RequestID(
		WithGenerator(func() (string, error) { return "genHdr", nil }),
	)).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Header should already be set for downstream handlers.
		if w.Header().Get(DefaultRequestIDHeader) != "genHdr" {
			t.Fatalf("expected response header to be set before next handler")
		}
		got, _ := RequestIDFromRequest(r)
		if got != "genHdr" {
			t.Fatalf("expected context id %q, got %q", "genHdr", got)
		}
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	h.ServeHTTP(rr, req)
}

func TestWithRequestID_And_RequestIDFromContext(t *testing.T) {
	ctx := WithRequestID(context.Background(), "x1")
	if got, ok := RequestIDFromContext(ctx); !ok || got != "x1" {
		t.Fatalf("expected ctx id %q, got %q (ok=%v)", "x1", got, ok)
	}

	// Empty id should not overwrite.
	ctx2 := WithRequestID(ctx, "")
	if got, ok := RequestIDFromContext(ctx2); !ok || got != "x1" {
		t.Fatalf("expected ctx id to remain %q, got %q (ok=%v)", "x1", got, ok)
	}
}

func TestRequestID_PanicsOnNilNext(t *testing.T) {
	mw := RequestID()
	defer func() {
		if p := recover(); p == nil {
			t.Fatalf("expected panic")
		}
	}()
	_ = mw(nil)
}

func TestRequestID_PanicsOnNilRequest(t *testing.T) {
	h := Chain(RequestID()).Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("should not reach next handler")
	}))

	defer func() {
		if p := recover(); p == nil {
			t.Fatalf("expected panic")
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), nil)
}

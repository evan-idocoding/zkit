package client

import (
	"net/http"
	"testing"
)

func TestSetHeader_SetsHeaderAndDoesNotMutateOriginalRequest(t *testing.T) {
	var got string
	base := RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		got = r.Header.Get("X-Test")
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	})

	rt := Chain(base, SetHeader("X-Test", "v1"))

	req := &http.Request{
		Method: http.MethodGet,
		URL:    mustURL(t, "http://example.com"),
		Header: make(http.Header),
	}

	_, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "v1" {
		t.Fatalf("got header=%q, want %q", got, "v1")
	}
	if req.Header.Get("X-Test") != "" {
		t.Fatalf("expected original request not mutated, got %q", req.Header.Get("X-Test"))
	}
}

func TestSetHeader_EmptyKey_IsNoop(t *testing.T) {
	base := &staticStatusRT{status: 200}
	rt := Chain(base, SetHeader("", "v1"))
	if rt != base {
		t.Fatalf("expected no-op middleware to return base roundtripper")
	}
}

package client

import (
	"net/http"
	"testing"
)

func TestChain_Order(t *testing.T) {
	base := RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	})

	var got []string
	mw := func(name string) Middleware {
		return func(next http.RoundTripper) http.RoundTripper {
			return RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
				got = append(got, "before:"+name)
				resp, err := next.RoundTrip(r)
				got = append(got, "after:"+name)
				return resp, err
			})
		}
	}

	rt := Chain(base, mw("a"), mw("b"), mw("c"))
	_, _ = rt.RoundTrip((&http.Request{Method: http.MethodGet, URL: mustURL(t, "http://example.com")}))

	want := []string{
		"before:a", "before:b", "before:c",
		"after:c", "after:b", "after:a",
	}
	if len(got) != len(want) {
		t.Fatalf("len(got)=%d, want %d; got=%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q, want %q; got=%v", i, got[i], want[i], got)
		}
	}
}

func TestChain_NilBase_UsesClonedDefaultTransportWhenPossible(t *testing.T) {
	rt := Chain(nil /* base */)
	if rt == nil {
		t.Fatalf("expected non-nil RoundTripper")
	}

	// When default is *http.Transport, we expect a different pointer (cloned).
	if dt, ok := http.DefaultTransport.(*http.Transport); ok && dt != nil {
		tp, ok := rt.(*http.Transport)
		if !ok {
			t.Fatalf("expected *http.Transport, got %T", rt)
		}
		if tp == dt {
			t.Fatalf("expected cloned transport (different pointer), got same as http.DefaultTransport")
		}
	}
}

func TestChain_SkipsNilMiddleware(t *testing.T) {
	base := RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	})
	mw := func(next http.RoundTripper) http.RoundTripper {
		return RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
			resp, err := next.RoundTrip(r)
			if err != nil {
				return nil, err
			}
			resp.StatusCode = 201
			return resp, nil
		})
	}

	rt := Chain(base, nil, mw, nil)
	resp, err := rt.RoundTrip(&http.Request{Method: http.MethodGet, URL: mustURL(t, "http://example.com")})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if resp.StatusCode != 201 {
		t.Fatalf("got %d, want 201", resp.StatusCode)
	}
}

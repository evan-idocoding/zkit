package client

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestNew_DefaultTransportIsIndependentWhenPossible(t *testing.T) {
	c := New()
	if c == nil {
		t.Fatalf("expected non-nil client")
	}
	if c.Transport == nil {
		t.Fatalf("expected non-nil transport")
	}

	if dt, ok := http.DefaultTransport.(*http.Transport); ok && dt != nil {
		tp, ok := c.Transport.(*http.Transport)
		if !ok {
			t.Fatalf("expected *http.Transport, got %T", c.Transport)
		}
		if tp == dt {
			t.Fatalf("expected cloned transport, got same pointer as http.DefaultTransport")
		}
	}
}

func TestNew_WithTimeout(t *testing.T) {
	c := New(WithTimeout(123 * time.Millisecond))
	if c.Timeout != 123*time.Millisecond {
		t.Fatalf("got %v, want %v", c.Timeout, 123*time.Millisecond)
	}
}

func TestNew_IgnoresNilOptions(t *testing.T) {
	c := New(nil, nil)
	if c == nil {
		t.Fatalf("expected non-nil client")
	}
	if c.Transport == nil {
		t.Fatalf("expected non-nil transport")
	}
}

func TestNew_WithCheckRedirect(t *testing.T) {
	fn := func(req *http.Request, via []*http.Request) error { return errors.New("no") }
	c := New(WithCheckRedirect(fn))
	if c.CheckRedirect == nil {
		t.Fatalf("expected non-nil CheckRedirect")
	}
	if c.CheckRedirect(&http.Request{}, nil) == nil {
		t.Fatalf("expected error from CheckRedirect")
	}
}

func TestNew_WithCookieJar(t *testing.T) {
	jar := &staticJar{}
	c := New(WithCookieJar(jar))
	if c.Jar != jar {
		t.Fatalf("expected jar to be set")
	}
}

func TestNew_WithTransportClones(t *testing.T) {
	in := &http.Transport{MaxIdleConns: 123}
	c := New(WithTransport(in))
	tp, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", c.Transport)
	}
	if tp == in {
		t.Fatalf("expected cloned transport, got same pointer")
	}
	if tp.MaxIdleConns != 123 {
		t.Fatalf("got MaxIdleConns=%d, want 123", tp.MaxIdleConns)
	}
}

func TestNew_WithRoundTripperTakesPrecedence(t *testing.T) {
	want := &staticStatusRT{status: 204}
	c := New(WithTransport(&http.Transport{MaxIdleConns: 123}), WithRoundTripper(want))
	if c.Transport != want {
		t.Fatalf("expected WithRoundTripper to take precedence")
	}
}

func TestNew_WithMiddlewaresWrapsTransport(t *testing.T) {
	base := RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	})
	c := New(
		WithRoundTripper(base),
		WithMiddlewares(func(next http.RoundTripper) http.RoundTripper {
			return RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
				resp, err := next.RoundTrip(r)
				if err != nil {
					return nil, err
				}
				resp.StatusCode = 201
				return resp, nil
			})
		}),
	)

	resp, err := c.Transport.RoundTrip(&http.Request{Method: http.MethodGet, URL: mustURL(t, "http://example.com")})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if resp.StatusCode != 201 {
		t.Fatalf("got %d, want 201", resp.StatusCode)
	}
}

func TestNew_WithMiddlewares_AppendsAcrossCallsAndSkipsNil(t *testing.T) {
	base := RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	})

	mw := func(delta int) Middleware {
		return func(next http.RoundTripper) http.RoundTripper {
			return RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
				resp, err := next.RoundTrip(r)
				if err != nil {
					return nil, err
				}
				resp.StatusCode += delta
				return resp, nil
			})
		}
	}

	// Expect: a(b(c(base))). With deltas: +1, +10, +100 => 200+100+10+1 = 311.
	c := New(
		WithRoundTripper(base),
		WithMiddlewares(mw(1)),
		WithMiddlewares(nil, mw(10), mw(100)),
	)

	resp, err := c.Transport.RoundTrip(&http.Request{Method: http.MethodGet, URL: mustURL(t, "http://example.com")})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if resp.StatusCode != 311 {
		t.Fatalf("got %d, want 311", resp.StatusCode)
	}
}

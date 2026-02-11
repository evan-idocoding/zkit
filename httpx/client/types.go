package client

import "net/http"

// Middleware wraps an http.RoundTripper and returns a new one.
//
// A common pattern is to implement cross-cutting concerns (metrics, tracing,
// header injection, custom error handling) as middlewares.
//
// Note: client does not provide any "business policy" middleware by design.
//
// Middleware must return a non-nil RoundTripper. The returned RoundTripper must be
// safe for concurrent use (RoundTrip may be called from multiple goroutines).
type Middleware func(http.RoundTripper) http.RoundTripper

// RoundTripperFunc adapts a function to an http.RoundTripper.
type RoundTripperFunc func(*http.Request) (*http.Response, error)

// RoundTrip implements http.RoundTripper.
func (f RoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

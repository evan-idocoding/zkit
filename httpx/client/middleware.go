package client

import "net/http"

// SetHeader returns a middleware that sets a request header for every request.
//
// It clones the request before mutation to avoid touching the original request.
// If key is empty, it returns a no-op middleware.
func SetHeader(key, value string) Middleware {
	if key == "" {
		return func(next http.RoundTripper) http.RoundTripper { return next }
	}
	return func(next http.RoundTripper) http.RoundTripper {
		return RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
			r2 := r.Clone(r.Context()) // clone to avoid touching the original request
			r2.Header.Set(key, value)
			return next.RoundTrip(r2)
		})
	}
}

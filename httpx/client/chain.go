package client

import "net/http"

// Chain applies middlewares to base and returns the wrapped RoundTripper.
//
// Order:
//   - Chain(base, a, b, c) returns a(b(c(base))).
//
// Nil base:
//   - If base is nil, Chain uses an independent transport cloned from
//     http.DefaultTransport (when it is *http.Transport).
func Chain(base http.RoundTripper, mws ...Middleware) http.RoundTripper {
	if base == nil {
		base = cloneDefaultTransport()
	}
	for i := len(mws) - 1; i >= 0; i-- {
		if mws[i] == nil {
			continue
		}
		base = mws[i](base)
	}
	return base
}

func cloneDefaultTransport() http.RoundTripper {
	// In Go's standard library, http.DefaultTransport is a *http.Transport.
	// We clone it to avoid global shared state and to "follow" stdlib defaults
	// without re-choosing defaults ourselves.
	if t, ok := http.DefaultTransport.(*http.Transport); ok && t != nil {
		return t.Clone()
	}
	// Extremely unlikely, but keep behavior usable.
	return http.DefaultTransport
}

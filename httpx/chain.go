package httpx

import "net/http"

// Middleware is a standard net/http middleware.
//
// A middleware wraps the next handler and returns a new handler.
type Middleware func(http.Handler) http.Handler

// Middlewares is a middleware chain builder.
//
// Order:
//   - Chain(a, b, c).Handler(h) returns a(b(c(h))).
type Middlewares []Middleware

// Chain creates a middleware chain from the provided middlewares.
//
// Nil middlewares are ignored.
func Chain(mws ...Middleware) Middlewares {
	if len(mws) == 0 {
		return nil
	}
	out := make([]Middleware, 0, len(mws))
	out = appendNonNil(out, mws)
	if len(out) == 0 {
		return nil
	}
	return out
}

// With returns a new chain by appending more middlewares to the current chain.
//
// Nil middlewares are ignored.
// With never mutates the receiver, and the returned chain does not share the
// underlying array with the receiver.
func (mws Middlewares) With(more ...Middleware) Middlewares {
	out := make([]Middleware, 0, len(mws)+len(more))
	out = appendNonNil(out, mws)
	out = appendNonNil(out, more)
	if len(out) == 0 {
		return nil
	}
	return out
}

// Handler builds and returns an http.Handler from the chain of middlewares,
// with h as the final handler.
//
// It panics if h is nil (a configuration/assembly error).
func (mws Middlewares) Handler(h http.Handler) http.Handler {
	if h == nil {
		panic("httpx: nil endpoint handler")
	}
	// Snapshot to keep the built handler stable even if the user later mutates
	// the Middlewares slice.
	snapshot := appendNonNil(nil, mws)
	return &ChainHandler{
		Endpoint:    h,
		chain:       chain(snapshot, h),
		Middlewares: snapshot,
	}
}

// HandlerFunc builds and returns an http.Handler from the chain of middlewares,
// with h as the final handler.
//
// It panics if h is nil (a configuration/assembly error).
func (mws Middlewares) HandlerFunc(h http.HandlerFunc) http.Handler {
	if h == nil {
		panic("httpx: nil endpoint handler func")
	}
	return mws.Handler(h)
}

// Wrap applies middlewares to h and returns the wrapped handler.
//
// Nil middlewares are ignored.
func Wrap(h http.Handler, mws ...Middleware) http.Handler {
	return Chain(mws...).Handler(h)
}

// ChainHandler is an http.Handler with support for handler composition and
// observation.
//
// ChainHandler is returned by Middlewares.Handler / Middlewares.HandlerFunc.
type ChainHandler struct {
	// Endpoint is the final handler.
	Endpoint http.Handler

	// chain is the pre-built composed handler.
	chain http.Handler

	// Middlewares is the middleware snapshot used to build chain (nil filtered).
	Middlewares Middlewares
}

func (c *ChainHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.chain.ServeHTTP(w, r)
}

func chain(mws Middlewares, endpoint http.Handler) http.Handler {
	if len(mws) == 0 {
		return endpoint
	}
	h := endpoint
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

func appendNonNil(dst []Middleware, src []Middleware) []Middleware {
	for _, mw := range src {
		if mw == nil {
			continue
		}
		dst = append(dst, mw)
	}
	return dst
}

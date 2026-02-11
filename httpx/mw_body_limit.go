// BodyLimit middleware.
//
// BodyLimit enforces a maximum request body size:
//   - It rejects early with 413 if Content-Length is known and exceeds the limit.
//   - Otherwise it wraps r.Body with http.MaxBytesReader, so reads past the limit return *http.MaxBytesError.
//
// Note: BodyLimit does not translate read-time errors into a response; downstream handlers that read
// the body must treat *http.MaxBytesError as 413 and stop processing.
//
// Minimal usage:
//
//	h := httpx.Wrap(finalHandler, httpx.BodyLimit(1<<20)) // 1 MiB
package httpx

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
)

// BodyLimitOption configures the BodyLimit middleware.
type BodyLimitOption func(*bodyLimitConfig)

type bodyLimitConfig struct {
	// maxBytes is the default max body size (bytes) to apply.
	maxBytes int64

	// limitFunc, if set, overrides maxBytes per-request.
	// If it returns ok=false, the middleware is skipped for that request.
	limitFunc BodyLimitFunc

	// onReject, if set, is called when BodyLimit rejects a request (early) or
	// detects a read-time limit exceed (late).
	onReject BodyLimitHandler
}

// BodyLimitFunc returns a per-request body size limit decision.
//
// If ok is false, BodyLimit is skipped for that request.
// If ok is true, n is used as the limit; if n <= 0, BodyLimit is also skipped.
type BodyLimitFunc func(r *http.Request) (n int64, ok bool)

// BodyLimitHandler is called when BodyLimit rejects or detects an exceeded limit.
//
// Observability-only: implementations must be fast and must not panic.
// Implementations must NOT write the response.
// If it panics, BodyLimit will swallow the panic and report it to stderr.
type BodyLimitHandler func(r *http.Request, info BodyLimitInfo)

// BodyLimitSource identifies where the body limit decision/rejection came from.
type BodyLimitSource string

const (
	// BodyLimitSourceContentLength indicates an early rejection based on Content-Length.
	BodyLimitSourceContentLength BodyLimitSource = "content-length"
	// BodyLimitSourceRead indicates a read-time detection via MaxBytesReader.
	BodyLimitSourceRead BodyLimitSource = "read"
)

// BodyLimitInfo describes a body limit rejection or exceed event.
type BodyLimitInfo struct {
	Limit         int64
	ContentLength int64
	Source        BodyLimitSource
}

// WithLimitFunc sets a per-request BodyLimitFunc.
//
// If fn is nil, it leaves the default unchanged.
func WithLimitFunc(fn BodyLimitFunc) BodyLimitOption {
	return func(c *bodyLimitConfig) {
		if fn != nil {
			c.limitFunc = fn
		}
	}
}

// WithOnReject sets a BodyLimitHandler called when a request is rejected due to
// Content-Length exceeding the limit, or when downstream reads exceed the limit.
//
// If fn is nil, it leaves the default unchanged.
func WithOnReject(fn BodyLimitHandler) BodyLimitOption {
	return func(c *bodyLimitConfig) {
		if fn != nil {
			c.onReject = fn
		}
	}
}

// BodyLimit returns a middleware that enforces a maximum request body size.
//
// Behavior:
//   - It computes a per-request limit from maxBytes and WithLimitFunc.
//   - If maxBytes <= 0, or BodyLimitFunc returns ok=false, or ok=true with n <= 0,
//     BodyLimit is skipped for that request.
//   - If Content-Length is known and exceeds the limit, it rejects early with
//     413 Request Entity Too Large, without calling downstream.
//     In this case, it also hints the client/proxy to close the connection to
//     avoid keep-alive reuse with an unread request body.
//   - Otherwise, it wraps r.Body with http.MaxBytesReader so downstream reads past
//     the limit fail with *http.MaxBytesError.
//
// OnReject trigger:
//   - If WithOnReject is provided, it is called for observability when:
//   - the request is rejected early (Source=content-length), or
//   - downstream reads exceed the limit (Source=read).
//   - For Source=read, OnReject is called after downstream returns, and only if
//     downstream actually read past the limit (i.e., observed a *http.MaxBytesError).
//   - If BodyLimit is skipped for a request, OnReject will not be called.
//
// Ordering:
//   - Place BodyLimit before any handler/middleware that may read or buffer the request body
//     (e.g., decompression, request body logging, multipart parsing, JSON decoding).
//
// Tuning/ops:
//   - WithLimitFunc is designed to work naturally with runtime-tunable configs.
//     A common convention is returning n <= 0 to temporarily disable the limit for a request.
//
// Downstream handling:
//
// BodyLimit does NOT translate read-time errors into a response. Downstream handlers
// that read r.Body should treat *http.MaxBytesError as 413 Request Entity Too Large
// (or the equivalent in your API) and must stop processing immediately to avoid
// partial-body bugs.
//
// Example:
//
//	b, err := io.ReadAll(r.Body)
//	if err != nil {
//		var mbe *http.MaxBytesError
//		if errors.As(err, &mbe) {
//			http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
//			return
//		}
//		http.Error(w, "bad request", http.StatusBadRequest)
//		return
//	}
func BodyLimit(maxBytes int64, opts ...BodyLimitOption) Middleware {
	cfg := bodyLimitConfig{maxBytes: maxBytes}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	return func(next http.Handler) http.Handler {
		if next == nil {
			panic("httpx: nil next handler")
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r == nil {
				panic("httpx: nil request")
			}

			limit := cfg.maxBytes
			if cfg.limitFunc != nil {
				n, ok := cfg.limitFunc(r)
				if !ok {
					next.ServeHTTP(w, r)
					return
				}
				limit = n
			}
			if limit <= 0 {
				next.ServeHTTP(w, r)
				return
			}

			// Early reject based on Content-Length when known and too large.
			if r.ContentLength > limit {
				info := BodyLimitInfo{
					Limit:         limit,
					ContentLength: r.ContentLength,
					Source:        BodyLimitSourceContentLength,
				}
				if cfg.onReject != nil {
					if p := callOnRejectNoPanic(cfg.onReject, r, info); p != nil {
						reportBodyLimitHookPanicToStderr(r, p)
					}
				}
				// We are not going to read the request body. Hint that the connection
				// should not be kept alive to avoid protocol confusion and unstable reuse.
				w.Header().Set("Connection", "close")
				if r.Body != nil {
					_ = r.Body.Close()
				}
				http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
				return
			}

			body := r.Body
			if body == nil {
				body = http.NoBody
			}

			limited := http.MaxBytesReader(w, body, limit)
			tracker := &bodyLimitReadCloser{
				rc:            limited,
				contentLength: r.ContentLength,
			}

			// Use a shallow copy (avoid Clone's deep copies); we only need to replace Body.
			r2 := r.WithContext(r.Context())
			r2.Body = tracker
			next.ServeHTTP(w, r2)

			if tracker.exceeded && cfg.onReject != nil {
				info := BodyLimitInfo{
					Limit:         limit,
					ContentLength: tracker.contentLength,
					Source:        BodyLimitSourceRead,
				}
				if p := callOnRejectNoPanic(cfg.onReject, r2, info); p != nil {
					reportBodyLimitHookPanicToStderr(r2, p)
				}
			}
		})
	}
}

type bodyLimitReadCloser struct {
	rc            io.ReadCloser
	contentLength int64
	exceeded      bool
}

func (b *bodyLimitReadCloser) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			b.exceeded = true
		}
	}
	return n, err
}

func (b *bodyLimitReadCloser) Close() error { return b.rc.Close() }

func callOnRejectNoPanic(fn BodyLimitHandler, r *http.Request, info BodyLimitInfo) (panicked any) {
	defer func() {
		if p := recover(); p != nil {
			panicked = p
		}
	}()
	fn(r, info)
	return nil
}

func reportBodyLimitHookPanicToStderr(r *http.Request, p any) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "httpx: BodyLimitHandler panicked")
	if r != nil {
		if r.Method != "" {
			fmt.Fprintf(&buf, " method=%s", r.Method)
		}
		if r.URL != nil {
			fmt.Fprintf(&buf, " url=%q", r.URL.String())
		}
	}
	fmt.Fprintf(&buf, " value=%v\n", p)

	// Serialize stderr writes (reuse the same package-level mutex used by Recover/Timeout).
	recoverStderrMu.Lock()
	_, _ = os.Stderr.Write(buf.Bytes())
	recoverStderrMu.Unlock()
}

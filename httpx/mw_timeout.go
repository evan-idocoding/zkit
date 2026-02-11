// Timeout middleware.
//
// Timeout derives a request context with a deadline and passes it downstream. It is cooperative:
// it does not write a response body, and it does not start goroutines; downstream code must respect
// context cancellation.
//
// When a derived context times out (DeadlineExceeded) after downstream returns, the optional
// TimeoutHandler (WithOnTimeout) can record observability signals (metrics/logs).
//
// Minimal usage:
//
//	h := httpx.Wrap(finalHandler, httpx.Timeout(2*time.Second))
package httpx

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"time"
)

// TimeoutOption configures the Timeout middleware.
type TimeoutOption func(*timeoutConfig)

type timeoutConfig struct {
	// timeout is the default timeout to apply.
	timeout time.Duration

	// timeoutFunc, if set, overrides timeout per-request.
	// If it returns ok=false, the middleware is skipped for that request.
	timeoutFunc TimeoutFunc

	// onTimeout, if set, is called after downstream returns and the derived context
	// ended with context.DeadlineExceeded.
	onTimeout TimeoutHandler

	// now is used for computing deadlines and measuring elapsed time.
	now func() time.Time
}

// TimeoutFunc returns a per-request timeout decision.
//
// If ok is false, Timeout will not derive a new context for this request.
// If ok is true, d is used as the timeout; if d <= 0, Timeout is also skipped.
type TimeoutFunc func(r *http.Request) (d time.Duration, ok bool)

// TimeoutHandler is called when a request times out (context deadline exceeded) and
// Timeout has derived the request context.
//
// Observability-only: implementations must be fast and must not panic.
// Implementations must NOT write the response.
// If a TimeoutHandler panics, Timeout will swallow the panic and report it to stderr.
type TimeoutHandler func(r *http.Request, info TimeoutInfo)

// TimeoutInfo describes a timeout event.
type TimeoutInfo struct {
	Timeout  time.Duration
	Deadline time.Time
	Elapsed  time.Duration
}

// WithTimeoutFunc sets a per-request TimeoutFunc.
//
// If fn is nil, it leaves the default unchanged.
func WithTimeoutFunc(fn TimeoutFunc) TimeoutOption {
	return func(c *timeoutConfig) {
		if fn != nil {
			c.timeoutFunc = fn
		}
	}
}

// WithOnTimeout sets a TimeoutHandler called when the derived context ends with
// context.DeadlineExceeded.
//
// If fn is nil, it leaves the default unchanged.
func WithOnTimeout(fn TimeoutHandler) TimeoutOption {
	return func(c *timeoutConfig) {
		if fn != nil {
			c.onTimeout = fn
		}
	}
}

// WithNow sets a custom clock function used by Timeout.
//
// This is primarily intended for tests. If fn is nil, it leaves the default unchanged.
func WithNow(fn func() time.Time) TimeoutOption {
	return func(c *timeoutConfig) {
		if fn != nil {
			c.now = fn
		}
	}
}

// Timeout returns a middleware that derives a request context with a timeout.
//
// Behavior:
//   - It derives a request context with deadline = now + timeout, unless:
//   - timeout <= 0, or
//   - TimeoutFunc returns ok=false, or d <= 0, or
//   - the incoming request context already has an earlier (or equal) deadline
//     (i.e., it never extends an existing deadline).
//   - It calls the downstream handler with the derived context when applied.
//   - After downstream returns, if the derived context ended with context.DeadlineExceeded,
//     it calls the TimeoutHandler (if provided) for observability.
//
// OnTimeout trigger:
//   - OnTimeout is called only when Timeout actually derived a context for the request.
//     If Timeout is skipped (by timeout <= 0 or TimeoutFunc), or if it keeps the parent
//     context due to an existing earlier/equal deadline, OnTimeout will not be called.
//
// Notes:
//   - Timeout does NOT write any response on timeout (standard-library flavored, low-risk).
//   - Timeout does NOT start a goroutine; it relies on cooperative cancellation via context.
func Timeout(timeout time.Duration, opts ...TimeoutOption) Middleware {
	cfg := timeoutConfig{
		timeout: timeout,
		now:     time.Now,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.now == nil {
		cfg.now = time.Now
	}

	return func(next http.Handler) http.Handler {
		if next == nil {
			panic("httpx: nil next handler")
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r == nil {
				panic("httpx: nil request")
			}

			d := cfg.timeout
			if cfg.timeoutFunc != nil {
				dd, ok := cfg.timeoutFunc(r)
				if !ok {
					next.ServeHTTP(w, r)
					return
				}
				d = dd
			}
			if d <= 0 {
				next.ServeHTTP(w, r)
				return
			}

			parent := r.Context()
			start := cfg.now()
			wantDeadline := start.Add(d)

			// Respect an existing earlier (or equal) deadline: never extend.
			if haveDeadline, ok := parent.Deadline(); ok && !wantDeadline.Before(haveDeadline) {
				// wantDeadline >= haveDeadline -> parent is earlier/equal; keep parent.
				next.ServeHTTP(w, r)
				return
			}

			ctx, cancel := context.WithDeadline(parent, wantDeadline)
			defer cancel()

			r2 := r.WithContext(ctx)
			next.ServeHTTP(w, r2)

			if cfg.onTimeout != nil && ctx.Err() == context.DeadlineExceeded {
				info := TimeoutInfo{
					Timeout:  d,
					Deadline: wantDeadline,
					Elapsed:  cfg.now().Sub(start),
				}
				if p := callOnTimeoutNoPanic(cfg.onTimeout, r2, info); p != nil {
					reportTimeoutHookPanicToStderr(r2, p)
				}
			}
		})
	}
}

func callOnTimeoutNoPanic(fn TimeoutHandler, r *http.Request, info TimeoutInfo) (panicked any) {
	defer func() {
		if p := recover(); p != nil {
			panicked = p
		}
	}()
	fn(r, info)
	return nil
}

func reportTimeoutHookPanicToStderr(r *http.Request, p any) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "httpx: TimeoutHandler panicked")
	if r != nil {
		if r.Method != "" {
			fmt.Fprintf(&buf, " method=%s", r.Method)
		}
		if r.URL != nil {
			fmt.Fprintf(&buf, " url=%q", r.URL.String())
		}
	}
	fmt.Fprintf(&buf, " value=%v\n", p)

	// Serialize stderr writes (reuse the same package-level mutex used by Recover).
	recoverStderrMu.Lock()
	_, _ = os.Stderr.Write(buf.Bytes())
	recoverStderrMu.Unlock()
}

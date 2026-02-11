// Recover middleware.
//
// Recover returns a middleware that recovers panics from downstream handlers and keeps the server alive.
//
// Behavior summary:
//   - It re-panics http.ErrAbortHandler to preserve net/http semantics.
//   - If the response has not started, it writes 500 Internal Server Error.
//   - Panics are reported via PanicHandler (WithOnPanic) or to stderr by default.
//
// Minimal usage:
//
//	h := httpx.Wrap(finalHandler, httpx.Recover())
package httpx

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"sync"
)

// RecoverOption configures the Recover middleware.
type RecoverOption func(*recoverConfig)

type recoverConfig struct {
	onPanic PanicHandler
}

// PanicHandler is called when the wrapped handler panics (except http.ErrAbortHandler).
//
// Implementations must be fast and must not panic. If a PanicHandler panics, Recover will
// swallow the secondary panic and report it to stderr as a fallback.
type PanicHandler func(r *http.Request, info RecoverInfo)

// RecoverInfo describes a recovered panic.
type RecoverInfo struct {
	Value any
	Stack []byte
}

// WithOnPanic sets a PanicHandler. If not set, Recover reports panics to stderr by default.
func WithOnPanic(fn PanicHandler) RecoverOption {
	return func(c *recoverConfig) { c.onPanic = fn }
}

// Recover returns a middleware that recovers from panics in downstream handlers.
//
// It re-panics http.ErrAbortHandler to preserve net/http semantics.
//
// If the response has not been written yet, it writes a 500 Internal Server Error.
// If the response has already started, it does not modify the response.
//
// Observability:
//   - If WithOnPanic is provided, it is called with the panic value and stack.
//   - Otherwise, panics are reported to stderr by default.
func Recover(opts ...RecoverOption) Middleware {
	cfg := recoverConfig{}
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
			sw := &recoverResponseWriter{w: w}

			defer func() {
				p := recover()
				if p == nil {
					return
				}
				if p == http.ErrAbortHandler {
					panic(p)
				}

				info := RecoverInfo{Value: p, Stack: debug.Stack()}
				if cfg.onPanic != nil {
					if p2 := callOnPanicNoPanic(cfg.onPanic, r, info); p2 != nil {
						// Secondary panic from user handler: keep server alive, but make it observable.
						reportRecoverPanicToStderr(r, RecoverInfo{
							Value: fmt.Sprintf("httpx: PanicHandler panicked: %v", p2),
							Stack: debug.Stack(),
						})
					}
				} else {
					reportRecoverPanicToStderr(r, info)
				}

				// Only write 500 if the response hasn't started yet.
				if !sw.wroteHeader {
					http.Error(sw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
			}()

			next.ServeHTTP(sw, r)
		})
	}
}

// recoverResponseWriter tracks whether the response has started.
// It forwards optional interfaces to avoid breaking features like streaming and hijacking.
type recoverResponseWriter struct {
	w           http.ResponseWriter
	wroteHeader bool
}

func (w *recoverResponseWriter) Header() http.Header { return w.w.Header() }

func (w *recoverResponseWriter) WriteHeader(statusCode int) {
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	w.w.WriteHeader(statusCode)
}

func (w *recoverResponseWriter) Write(p []byte) (int, error) {
	// net/http will implicitly write headers on first Write.
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	return w.w.Write(p)
}

// Unwrap returns the underlying ResponseWriter.
func (w *recoverResponseWriter) Unwrap() http.ResponseWriter { return w.w }

// Flush implements http.Flusher if supported by the underlying ResponseWriter.
func (w *recoverResponseWriter) Flush() {
	if f, ok := w.w.(http.Flusher); ok {
		// In real net/http, Flush may send headers; treat it as response started.
		if !w.wroteHeader {
			w.wroteHeader = true
		}
		f.Flush()
	}
}

// Hijack implements http.Hijacker if supported by the underlying ResponseWriter.
func (w *recoverResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.w.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("httpx: underlying ResponseWriter does not support hijacking")
	}
	c, rw, err := h.Hijack()
	if err == nil && !w.wroteHeader {
		// After hijacking, we must not try to write an HTTP response.
		w.wroteHeader = true
	}
	return c, rw, err
}

// Push implements http.Pusher if supported by the underlying ResponseWriter.
func (w *recoverResponseWriter) Push(target string, opts *http.PushOptions) error {
	p, ok := w.w.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return p.Push(target, opts)
}

// ReadFrom implements io.ReaderFrom if supported by the underlying ResponseWriter.
func (w *recoverResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	rf, ok := w.w.(io.ReaderFrom)
	if !ok {
		// Use the wrapper so Write marks wroteHeader.
		return io.Copy(w, r)
	}
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	return rf.ReadFrom(r)
}

var recoverStderrMu sync.Mutex

func reportRecoverPanicToStderr(r *http.Request, info RecoverInfo) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "httpx: panic")
	if r != nil {
		if r.Method != "" {
			fmt.Fprintf(&buf, " method=%s", r.Method)
		}
		if r.URL != nil {
			fmt.Fprintf(&buf, " url=%q", r.URL.String())
		}
	}
	fmt.Fprintf(&buf, " value=%v\n", info.Value)
	if len(info.Stack) > 0 {
		_, _ = buf.Write(info.Stack)
		if info.Stack[len(info.Stack)-1] != '\n' {
			_ = buf.WriteByte('\n')
		}
	}

	recoverStderrMu.Lock()
	_, _ = os.Stderr.Write(buf.Bytes())
	recoverStderrMu.Unlock()
}

func callOnPanicNoPanic(fn PanicHandler, r *http.Request, info RecoverInfo) (panicked any) {
	defer func() {
		if p := recover(); p != nil {
			panicked = p
		}
	}()
	fn(r, info)
	return nil
}

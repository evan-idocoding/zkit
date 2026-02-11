// RequestID middleware.
//
// RequestID ensures each request has a request id, stored in context and (by default) echoed in the
// response header.
//
// It is designed to be safe-by-default:
//   - Incoming values are validated to avoid header/log pollution.
//   - If incoming ids are not trusted/valid, it generates a new one.
//
// Minimal usage:
//
//	h := httpx.Wrap(finalHandler, httpx.RequestID())
//
// Extracting:
//
//	id, _ := httpx.RequestIDFromRequest(r)
package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// DefaultRequestIDHeader is the default header used for request ID propagation.
const DefaultRequestIDHeader = "X-Request-ID"

// RequestIDOption configures the RequestID middleware.
type RequestIDOption func(*requestIDConfig)

type requestIDConfig struct {
	// incomingHeaders are checked in order for an incoming request id.
	incomingHeaders []string

	// trustIncoming controls whether incoming headers can be used as the request id.
	// If false, RequestID always generates a new id.
	trustIncoming bool

	// setResponseHeader controls whether the response header is set.
	setResponseHeader bool

	// maxLen is the maximum allowed length for an incoming request id.
	maxLen int

	// validateIncoming checks whether an incoming request id is acceptable.
	validateIncoming func(string) bool

	// validateGenerated checks whether a generated request id is acceptable.
	// This is intentionally independent from maxLen (which is about incoming values).
	validateGenerated func(string) bool

	// gen generates a new request id.
	gen RequestIDGenerator
}

// RequestIDGenerator generates a new request id.
//
// Implementations must be fast and should avoid allocations.
// If it returns an error or an invalid/empty id, RequestID falls back to an internal generator.
type RequestIDGenerator func() (string, error)

// WithIncomingHeaders sets the list of header names to look for an incoming request id, in order.
//
// Empty/blank names are ignored. If the resulting list is empty, it falls back to the default
// (X-Request-ID).
func WithIncomingHeaders(headers []string) RequestIDOption {
	return func(c *requestIDConfig) {
		if len(headers) == 0 {
			c.incomingHeaders = nil
			return
		}
		out := make([]string, 0, len(headers))
		for _, h := range headers {
			h = strings.TrimSpace(h)
			if h == "" {
				continue
			}
			out = append(out, h)
		}
		c.incomingHeaders = out
	}
}

// WithTrustIncoming controls whether RequestID trusts and uses incoming request id headers.
//
// Default is true.
func WithTrustIncoming(v bool) RequestIDOption {
	return func(c *requestIDConfig) { c.trustIncoming = v }
}

// WithSetResponseHeader controls whether RequestID sets DefaultRequestIDHeader on the response.
//
// Default is true.
func WithSetResponseHeader(v bool) RequestIDOption {
	return func(c *requestIDConfig) { c.setResponseHeader = v }
}

// WithMaxLen sets the maximum allowed length for an incoming request id.
//
// If n <= 0, it leaves the default unchanged.
func WithMaxLen(n int) RequestIDOption {
	return func(c *requestIDConfig) {
		if n > 0 {
			c.maxLen = n
		}
	}
}

// WithValidator sets a custom validator for incoming request ids.
//
// If fn is nil, it leaves the default validator unchanged.
func WithValidator(fn func(string) bool) RequestIDOption {
	return func(c *requestIDConfig) {
		if fn != nil {
			c.validateIncoming = fn
		}
	}
}

// WithGenerator sets a custom request id generator.
//
// If fn is nil, it leaves the default generator unchanged.
func WithGenerator(fn RequestIDGenerator) RequestIDOption {
	return func(c *requestIDConfig) {
		if fn != nil {
			c.gen = fn
		}
	}
}

// RequestID returns a middleware that ensures each request has a request id.
//
// Behavior:
//   - Incoming: by default, it reads X-Request-ID (or the configured incoming headers), validates
//     it, and uses the first valid value (in configured order).
//   - If no valid incoming id is found (or trustIncoming is false), it generates a new id.
//   - It stores the request id in the request context for downstream handlers.
//   - It sets the response header X-Request-ID (DefaultRequestIDHeader) by default.
//
// Defaults are chosen to be explicit and safe:
//   - Only X-Request-ID is checked by default; users can extend via WithIncomingHeaders.
//   - Incoming ids are validated (length + allowed characters) to avoid log/header pollution.
//   - The response header is set by default for easier debugging.
func RequestID(opts ...RequestIDOption) Middleware {
	cfg := requestIDConfig{
		incomingHeaders:   []string{DefaultRequestIDHeader},
		trustIncoming:     true,
		setResponseHeader: true,
		maxLen:            128,
	}
	cfg.validateIncoming = func(s string) bool { return defaultValidateRequestID(s, cfg.maxLen) }
	cfg.validateGenerated = func(s string) bool { return defaultValidateRequestID(s, 256) }
	cfg.gen = defaultRequestIDGenerator

	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if len(cfg.incomingHeaders) == 0 {
		cfg.incomingHeaders = []string{DefaultRequestIDHeader}
	}
	if cfg.validateIncoming == nil {
		cfg.validateIncoming = func(s string) bool { return defaultValidateRequestID(s, cfg.maxLen) }
	}
	if cfg.validateGenerated == nil {
		cfg.validateGenerated = func(s string) bool { return defaultValidateRequestID(s, 256) }
	}
	if cfg.gen == nil {
		cfg.gen = defaultRequestIDGenerator
	}

	return func(next http.Handler) http.Handler {
		if next == nil {
			panic("httpx: nil next handler")
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r == nil {
				// Like other httpx middlewares, treat nil inputs as an assembly/config error.
				panic("httpx: nil request")
			}
			id := ""
			if cfg.trustIncoming {
				for _, h := range cfg.incomingHeaders {
					// Only accept a single header value to avoid ambiguous/conflicting ids.
					// If a client/proxy sends multiple values, treat it as invalid and fall back
					// to generation.
					vs := r.Header.Values(h)
					if len(vs) != 1 {
						continue
					}
					v := vs[0]
					if v == "" {
						continue
					}
					if cfg.validateIncoming(v) {
						id = v
						break
					}
				}
			}
			if id == "" {
				id = generateRequestID(cfg.gen, cfg.validateGenerated)
			}

			if cfg.setResponseHeader {
				w.Header().Set(DefaultRequestIDHeader, id)
			}
			ctx := context.WithValue(r.Context(), requestIDKey{}, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type requestIDKey struct{}

// RequestIDFromContext extracts the request id from ctx.
func RequestIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	v, ok := ctx.Value(requestIDKey{}).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// RequestIDFromRequest extracts the request id from r.Context().
func RequestIDFromRequest(r *http.Request) (string, bool) {
	if r == nil {
		return "", false
	}
	return RequestIDFromContext(r.Context())
}

// WithRequestID returns a derived context with id stored as the request id.
//
// If id is empty, it returns ctx unchanged.
func WithRequestID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey{}, id)
}

func defaultValidateRequestID(s string, maxLen int) bool {
	if s == "" {
		return false
	}
	if maxLen > 0 && len(s) > maxLen {
		return false
	}
	// Reject comma/whitespace to avoid ambiguous/combined header values and log pollution.
	for i := 0; i < len(s); i++ {
		b := s[i]
		switch {
		case b >= 'a' && b <= 'z':
		case b >= 'A' && b <= 'Z':
		case b >= '0' && b <= '9':
		case b == '.' || b == '_' || b == '-':
		default:
			return false
		}
	}
	return true
}

func generateRequestID(gen RequestIDGenerator, validate func(string) bool) string {
	// First try user generator.
	if gen != nil {
		if s, err := gen(); err == nil && s != "" && (validate == nil || validate(s)) {
			return s
		}
	}
	// Fallback.
	if s, err := defaultRequestIDGenerator(); err == nil && s != "" && (validate == nil || validate(s)) {
		return s
	}
	// Last-resort non-crypto fallback (should be extremely rare).
	return fallbackRequestID()
}

func defaultRequestIDGenerator() (string, error) {
	var b [16]byte
	_, err := rand.Read(b[:])
	if err != nil {
		return "", err
	}
	var out [32]byte
	hex.Encode(out[:], b[:])
	return string(out[:]), nil
}

var fallbackCounter uint64

func fallbackRequestID() string {
	// Keep it ASCII and validator-friendly.
	// Format: <hex unix nanos>-<hex counter>
	n := time.Now().UnixNano()
	c := atomic.AddUint64(&fallbackCounter, 1)
	return strings.ToLower(strconv.FormatInt(n, 16)) + "-" + strings.ToLower(strconv.FormatUint(c, 16))
}

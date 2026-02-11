// CORS middleware.
//
// CORS adds CORS headers for browser clients and may short-circuit successful preflight requests
// (OPTIONS + Access-Control-Request-Method).
//
// Defaults are intentionally debug-friendly (allow any Origin). For production, you typically want
// to configure WithAllowedOrigins and optionally restrict methods/headers.
//
// Ordering note:
// Place CORS before authentication/authorization middleware when you want preflight requests to
// succeed; otherwise browsers may fail cross-origin requests.
//
// Minimal usage:
//
//	h := httpx.Wrap(finalHandler, httpx.CORS())
package httpx

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// CORSOption configures the CORS middleware.
type CORSOption func(*corsConfig)

type corsConfig struct {
	// enabledFunc controls whether CORS is enabled for a request.
	// If it returns false, CORS is skipped (no headers written, no preflight short-circuit).
	enabledFunc func(r *http.Request) bool

	// matchFunc controls whether CORS applies to a request.
	// If it returns false, CORS is skipped (no headers written, no preflight short-circuit).
	matchFunc func(r *http.Request) bool

	// allowCredentials controls whether Access-Control-Allow-Credentials: true is set.
	allowCredentials bool

	// allowNullOrigin controls whether Origin: "null" is allowed when an allowlist
	// is configured (patterns != nil).
	//
	// Note: When patterns is nil (default "allow any"), Origin:"null" is allowed regardless
	// of this flag (debug-friendly).
	allowNullOrigin bool

	// maxAge controls Access-Control-Max-Age for preflight responses.
	// If <= 0, the header is not set.
	maxAge time.Duration

	// preflightStatus is the status code used for successful preflight responses.
	// Allowed values: 200 or 204. Default is 204.
	preflightStatus int

	// exposeHeadersValue is the value for Access-Control-Expose-Headers.
	// If empty, the header is not set.
	exposeHeadersValue string

	// allowedMethods controls allowed methods for CORS.
	//
	// If nil, methods are not restricted.
	// If non-nil but empty, no method is allowed (fail-closed on invalid config).
	allowedMethods map[string]struct{}

	// allowedHeaders controls allowed request headers for preflight.
	//
	// If nil, headers are not restricted (reflected from request).
	// If non-nil but empty, no header is allowed (fail-closed on invalid config).
	allowedHeaders map[string]struct{}

	// allowedOriginPatterns is an allowlist of origin/host patterns.
	//
	// If nil, any Origin is allowed (debug-friendly).
	// If non-nil but empty, no Origin is allowed (fail-closed when user configured an invalid list).
	//
	// Patterns match against the request Origin's hostname only (scheme and port are ignored):
	//   - "example.com" matches "example.com" AND any subdomain (e.g. "a.example.com", "a.b.example.com").
	//   - "*.example.com" matches any subdomain (e.g. "a.example.com", "a.b.example.com") BUT NOT "example.com".
	//   - "*.a.example.com" matches "b.a.example.com" and deeper BUT NOT "a.example.com".
	//
	// For convenience, patterns may also be full origins (e.g. "https://example.com:8443");
	// only the hostname part is used for matching.
	allowedOriginPatterns []originPattern
}

type originPattern struct {
	base             string
	requireSubdomain bool
}

// CountValidOriginPatterns counts how many entries in origins are valid origin patterns.
//
// It follows the same parsing rules as WithAllowedOrigins:
//   - empty/blank entries are ignored
//   - entries may be hostname patterns (example.com, *.example.com) or full origins
//     (https://example.com:8443), where only hostname is used
//
// The returned count is typically used for strict, fail-fast validation in assembly layers.
func CountValidOriginPatterns(origins []string) int {
	n := 0
	for _, raw := range origins {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		if _, ok := parseOriginPattern(s); ok {
			n++
		}
	}
	return n
}

// ValidateOriginPatterns performs a strict validation for origins used by WithAllowedOrigins.
//
// It returns nil when:
//   - origins is empty (meaning "allow any Origin"), or
//   - at least one valid origin pattern is parsed.
//
// It returns an error when origins is non-empty but no valid patterns can be parsed after
// ignoring blank entries. This is intended for fail-fast assembly layers to avoid accidentally
// denying all origins due to typos.
func ValidateOriginPatterns(origins []string) error {
	if len(origins) == 0 {
		return nil
	}
	if CountValidOriginPatterns(origins) > 0 {
		return nil
	}
	return fmt.Errorf("httpx: invalid allowed origins: no valid patterns parsed")
}

// WithEnabledFunc sets a per-request toggle for CORS.
//
// If fn is nil, it leaves the default unchanged.
//
// When fn returns false, CORS is skipped entirely:
//   - no CORS headers are written
//   - preflight requests are not short-circuited
func WithEnabledFunc(fn func(r *http.Request) bool) CORSOption {
	return func(c *corsConfig) {
		if fn != nil {
			c.enabledFunc = fn
		}
	}
}

// WithMatchFunc sets a per-request matcher for CORS.
//
// If fn is nil, it leaves the default unchanged.
//
// When fn returns false, CORS is skipped entirely:
//   - no CORS headers are written
//   - preflight requests are not short-circuited
func WithMatchFunc(fn func(r *http.Request) bool) CORSOption {
	return func(c *corsConfig) {
		if fn != nil {
			c.matchFunc = fn
		}
	}
}

// WithAllowCredentials controls whether Access-Control-Allow-Credentials is set.
//
// Default is true (debug-friendly).
func WithAllowCredentials(v bool) CORSOption {
	return func(c *corsConfig) { c.allowCredentials = v }
}

// WithAllowNullOrigin controls whether Origin: "null" is allowed when an allowlist
// is configured via WithAllowedOrigins.
//
// Default is false (when allowlist is configured, Origin:"null" is rejected unless enabled).
func WithAllowNullOrigin(v bool) CORSOption {
	return func(c *corsConfig) { c.allowNullOrigin = v }
}

// WithMaxAge controls Access-Control-Max-Age for preflight responses.
//
// Default is 10 minutes (debug-friendly, reduces repeated preflight requests).
// If d <= 0, Access-Control-Max-Age is not set.
func WithMaxAge(d time.Duration) CORSOption {
	return func(c *corsConfig) { c.maxAge = d }
}

// WithPreflightStatus sets the status code for successful preflight responses.
//
// Allowed values are 200 (OK) and 204 (No Content). Default is 204.
// Invalid values are ignored and keep the default unchanged.
func WithPreflightStatus(code int) CORSOption {
	return func(c *corsConfig) {
		switch code {
		case http.StatusOK, http.StatusNoContent:
			c.preflightStatus = code
		}
	}
}

// WithExposeHeaders sets Access-Control-Expose-Headers for non-preflight responses.
//
// Default is exposing X-Request-ID (httpx DefaultRequestIDHeader), so browser clients can
// read it via fetch/XHR (useful for correlating logs and debugging).
//
// If headers is nil or empty, Access-Control-Expose-Headers is not set (disabled).
// Empty/blank entries are ignored; duplicates are removed (case-insensitive).
func WithExposeHeaders(headers []string) CORSOption {
	return func(c *corsConfig) {
		c.exposeHeadersValue = ""
		applyExposeHeaders(c, headers, false)
	}
}

// WithExposeHeadersAppend appends headers to Access-Control-Expose-Headers.
//
// It appends to the current expose list (default starts with X-Request-ID). If the expose
// header was disabled via WithExposeHeaders(nil), Append will start from empty.
// Empty/blank entries are ignored; duplicates are removed (case-insensitive).
func WithExposeHeadersAppend(headers []string) CORSOption {
	return func(c *corsConfig) {
		applyExposeHeaders(c, headers, true)
	}
}

// WithAllowedMethods restricts allowed methods for CORS.
//
// If methods is nil or empty, methods are not restricted (default).
// If methods is non-empty but no valid methods are parsed, it denies all methods (fail-closed).
//
// Enforcement:
//   - For preflight: Access-Control-Request-Method must be allowed, otherwise CORS is skipped.
//   - For non-preflight: the request method must be allowed, otherwise CORS is skipped.
func WithAllowedMethods(methods []string) CORSOption {
	return func(c *corsConfig) {
		if len(methods) == 0 {
			c.allowedMethods = nil
			return
		}
		m := make(map[string]struct{}, len(methods))
		for _, raw := range methods {
			s := strings.TrimSpace(raw)
			if s == "" {
				continue
			}
			s = strings.ToUpper(s)
			m[s] = struct{}{}
		}
		// Keep an empty (non-nil) map to represent deny-all on invalid config.
		c.allowedMethods = m
	}
}

// WithAllowedHeaders restricts allowed request headers for CORS preflight.
//
// If headers is nil or empty, headers are not restricted (default; reflected from request).
// If headers is non-empty but no valid headers are parsed, it denies all headers (fail-closed).
//
// Enforcement:
//   - For preflight: all headers in Access-Control-Request-Headers must be allowed,
//     otherwise CORS is skipped.
func WithAllowedHeaders(headers []string) CORSOption {
	return func(c *corsConfig) {
		if len(headers) == 0 {
			c.allowedHeaders = nil
			return
		}
		m := make(map[string]struct{}, len(headers))
		for _, raw := range headers {
			s := strings.TrimSpace(raw)
			if s == "" {
				continue
			}
			m[strings.ToLower(s)] = struct{}{}
		}
		// Keep an empty (non-nil) map to represent deny-all on invalid config.
		c.allowedHeaders = m
	}
}

// WithAllowedOrigins sets the allowlist of allowed origins/host patterns.
//
// If origins is nil or empty, it means "allow any Origin" (default; debug-friendly).
// Empty/blank entries are ignored.
//
// Safety note:
//   - If origins is non-empty but none of the entries are valid patterns after parsing,
//     the middleware will deny all origins (fail-closed) to avoid accidental "allow any"
//     due to configuration typos.
//
// Matching is based on the request Origin's hostname only (scheme and port are ignored).
// Supported patterns:
//   - "example.com": matches "example.com" and any subdomain.
//   - "*.example.com": matches any subdomain under "example.com", but not "example.com" itself.
//   - "*.a.example.com": matches any subdomain under "a.example.com", but not "a.example.com" itself.
//
// For convenience, entries may also be full origins like "https://example.com:8443"; only the hostname
// part is used.
func WithAllowedOrigins(origins []string) CORSOption {
	return func(c *corsConfig) {
		if len(origins) == 0 {
			c.allowedOriginPatterns = nil
			return
		}
		out := make([]originPattern, 0, len(origins))
		for _, raw := range origins {
			s := strings.TrimSpace(raw)
			if s == "" {
				continue
			}
			p, ok := parseOriginPattern(s)
			if !ok {
				continue
			}
			out = append(out, p)
		}
		// Fail-closed: user configured a non-empty list, but no valid patterns parsed.
		// Keep a non-nil empty slice to represent "deny all".
		c.allowedOriginPatterns = out
	}
}

// CORS returns a middleware that adds CORS headers for browser clients.
//
// Defaults are intentionally "debug-friendly":
//   - Any Origin is allowed by default (Origin is reflected).
//   - Access-Control-Allow-Credentials is enabled by default.
//   - Preflight (OPTIONS + Access-Control-Request-Method) is short-circuited by default
//     with 204 No Content.
//   - Preflight responses include Access-Control-Max-Age by default (10 minutes).
//   - Access-Control-Expose-Headers is set by default to expose X-Request-ID.
//
// Safety and predictability:
//   - CORS is applied only when the request has exactly one non-empty Origin header value.
//   - If WithAllowedOrigins is set to a non-empty list, the Origin hostname must match
//     one of the allowed patterns (scheme and port are ignored).
//   - When the origin is not allowed (or the Origin header is invalid), the middleware
//     does not write any CORS headers.
//
// Preflight behavior:
//   - A request is considered preflight when method is OPTIONS and the request has a
//     non-empty Access-Control-Request-Method header.
//   - For allowed preflight requests, it writes CORS preflight response headers and
//     returns 204 without calling downstream.
//   - For disallowed preflight requests, it does not short-circuit; downstream decides.
//
// Ordering:
//   - Place CORS before authentication/authorization middleware when you want preflight
//     requests to succeed. If a preflight request reaches an auth middleware first, it
//     may be rejected, causing browsers to fail the actual cross-origin request.
//
// Tuning:
//   - WithEnabledFunc and WithMatchFunc are designed to work naturally with runtime
//     tuning flags: return false to temporarily disable CORS for a request.
func CORS(opts ...CORSOption) Middleware {
	cfg := corsConfig{
		allowCredentials:   true,
		allowNullOrigin:    false,
		maxAge:             10 * time.Minute,
		preflightStatus:    http.StatusNoContent,
		exposeHeadersValue: DefaultRequestIDHeader,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.preflightStatus == 0 {
		cfg.preflightStatus = http.StatusNoContent
	}

	return func(next http.Handler) http.Handler {
		if next == nil {
			panic("httpx: nil next handler")
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r == nil {
				panic("httpx: nil request")
			}

			if cfg.enabledFunc != nil && !cfg.enabledFunc(r) {
				next.ServeHTTP(w, r)
				return
			}
			if cfg.matchFunc != nil && !cfg.matchFunc(r) {
				next.ServeHTTP(w, r)
				return
			}

			origin, ok := singleHeaderValue(r.Header, "Origin")
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}
			if !isOriginAllowed(origin, cfg.allowedOriginPatterns, cfg.allowNullOrigin) {
				next.ServeHTTP(w, r)
				return
			}

			preflight := isPreflight(r)

			// Apply restrictions before writing any headers.
			if preflight {
				acrm := strings.TrimSpace(r.Header.Get("Access-Control-Request-Method"))
				if !isMethodAllowed(acrm, cfg.allowedMethods) {
					next.ServeHTTP(w, r)
					return
				}
				acrhJoined := joinHeaderValues(r.Header, "Access-Control-Request-Headers")
				if !areRequestHeadersAllowed(acrhJoined, cfg.allowedHeaders) {
					next.ServeHTTP(w, r)
					return
				}

				// Now we can safely write preflight headers.
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				addVary(h, "Origin")
				if cfg.allowCredentials {
					h.Set("Access-Control-Allow-Credentials", "true")
				}
				if acrm != "" {
					h.Set("Access-Control-Allow-Methods", acrm)
					addVary(h, "Access-Control-Request-Method")
				}
				if strings.TrimSpace(acrhJoined) != "" {
					h.Set("Access-Control-Allow-Headers", acrhJoined)
					addVary(h, "Access-Control-Request-Headers")
				}
				if cfg.maxAge > 0 {
					sec := int64(cfg.maxAge / time.Second)
					if sec > 0 {
						h.Set("Access-Control-Max-Age", strconv.FormatInt(sec, 10))
					}
				}
				w.WriteHeader(cfg.preflightStatus)
				return
			}

			if !isMethodAllowed(r.Method, cfg.allowedMethods) {
				next.ServeHTTP(w, r)
				return
			}

			// Apply common CORS response headers.
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			addVary(h, "Origin")
			if cfg.allowCredentials {
				h.Set("Access-Control-Allow-Credentials", "true")
			}
			if cfg.exposeHeadersValue != "" {
				h.Set("Access-Control-Expose-Headers", cfg.exposeHeadersValue)
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isPreflight(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.Method != http.MethodOptions {
		return false
	}
	// ACRM is required for a CORS preflight request.
	return strings.TrimSpace(r.Header.Get("Access-Control-Request-Method")) != ""
}

func isOriginAllowed(origin string, patterns []originPattern, allowNull bool) bool {
	// Default: allow any origin (debug-friendly).
	if patterns == nil {
		return true
	}
	// Fail-closed: explicitly configured empty (no valid patterns).
	if len(patterns) == 0 {
		return false
	}
	if strings.EqualFold(origin, "null") {
		return allowNull
	}
	host, ok := originHostname(origin)
	if !ok || host == "" {
		return false
	}
	for _, p := range patterns {
		if matchHostnamePattern(host, p) {
			return true
		}
	}
	return false
}

func matchHostnamePattern(host string, p originPattern) bool {
	if host == "" || p.base == "" {
		return false
	}
	host = strings.ToLower(host)
	base := strings.ToLower(p.base)
	if host == base {
		return !p.requireSubdomain
	}
	// Require a dot boundary to avoid matching "evil-example.com" for "example.com".
	return strings.HasSuffix(host, "."+base)
}

func parseOriginPattern(s string) (originPattern, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return originPattern{}, false
	}
	if strings.HasPrefix(s, "*.") {
		base, ok := normalizeHostLike(s[2:])
		if !ok {
			return originPattern{}, false
		}
		return originPattern{base: base, requireSubdomain: true}, true
	}
	base, ok := normalizeHostLike(s)
	if !ok {
		return originPattern{}, false
	}
	return originPattern{base: base, requireSubdomain: false}, true
}

// originHostname parses Origin (scheme://host[:port]) and returns its normalized hostname.
func originHostname(origin string) (string, bool) {
	u, err := url.Parse(origin)
	if err != nil || u == nil {
		return "", false
	}
	// Origin must be an absolute URL with scheme and host. "null" or relative forms are invalid here.
	if u.Scheme == "" || u.Host == "" {
		return "", false
	}
	return normalizeHostname(u.Hostname())
}

// normalizeHostLike accepts either a hostname (optionally with :port), or a full origin URL.
// It returns a normalized hostname (lowercased, without trailing dot).
func normalizeHostLike(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	// If it looks like a URL, parse and extract hostname.
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil || u == nil || u.Host == "" {
			return "", false
		}
		return normalizeHostname(u.Hostname())
	}
	// Strip port if present.
	if host, _, err := net.SplitHostPort(s); err == nil {
		s = host
	}
	// Strip brackets for IPv6 without port (e.g. "[::1]").
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") && len(s) > 2 {
		s = s[1 : len(s)-1]
	}
	return normalizeHostname(s)
}

func normalizeHostname(host string) (string, bool) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", false
	}
	host = strings.ToLower(host)
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return "", false
	}
	// Reject obvious non-host strings (keep this lightweight).
	if strings.ContainsAny(host, " \t\r\n/") {
		return "", false
	}
	return host, true
}

func applyExposeHeaders(c *corsConfig, headers []string, appendMode bool) {
	if c == nil {
		return
	}
	if !appendMode {
		c.exposeHeadersValue = ""
	}
	if len(headers) == 0 {
		return
	}
	seen := make(map[string]struct{})
	// Keep deterministic output order: start from current value (if any), then append new.
	out := make([]string, 0)
	if c.exposeHeadersValue != "" {
		for _, tok := range strings.Split(c.exposeHeadersValue, ",") {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}
			k := strings.ToLower(tok)
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			out = append(out, tok)
		}
	}
	for _, raw := range headers {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		k := strings.ToLower(s)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 {
		c.exposeHeadersValue = ""
		return
	}
	c.exposeHeadersValue = strings.Join(out, ", ")
}

func isMethodAllowed(method string, allowed map[string]struct{}) bool {
	method = strings.TrimSpace(method)
	if method == "" {
		return false
	}
	// Not restricted.
	if allowed == nil {
		return true
	}
	// Fail-closed (invalid config): deny all.
	if len(allowed) == 0 {
		return false
	}
	_, ok := allowed[strings.ToUpper(method)]
	return ok
}

func areRequestHeadersAllowed(acrhJoined string, allowed map[string]struct{}) bool {
	// Not restricted: always ok.
	if allowed == nil {
		return true
	}
	// Fail-closed (invalid config): deny all.
	if len(allowed) == 0 {
		// But if request asks for none, allow.
		return strings.TrimSpace(acrhJoined) == ""
	}
	acrhJoined = strings.TrimSpace(acrhJoined)
	if acrhJoined == "" {
		return true
	}
	parts := strings.Split(acrhJoined, ",")
	for _, p := range parts {
		h := strings.TrimSpace(p)
		if h == "" {
			continue
		}
		if _, ok := allowed[strings.ToLower(h)]; !ok {
			return false
		}
	}
	return true
}

// singleHeaderValue returns the single header value for name if it exists and is unambiguous.
// If the header is absent, it returns ok=false.
// If multiple values exist, it returns ok=false (treat as invalid/ambiguous).
func singleHeaderValue(h http.Header, name string) (value string, ok bool) {
	if h == nil {
		return "", false
	}
	vs := h.Values(name)
	if len(vs) == 0 {
		return "", false
	}
	if len(vs) != 1 {
		return "", false
	}
	return vs[0], true
}

// joinHeaderValues joins all values of a request header into a single comma-separated string.
// This is useful for headers like Access-Control-Request-Headers which may appear multiple times.
func joinHeaderValues(h http.Header, name string) string {
	if h == nil {
		return ""
	}
	vs := h.Values(name)
	if len(vs) == 0 {
		return ""
	}
	if len(vs) == 1 {
		return strings.TrimSpace(vs[0])
	}
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	return strings.Join(out, ", ")
}

// addVary appends a value to the Vary response header if not already present.
// It keeps existing values and avoids duplicates (case-insensitive).
func addVary(h http.Header, value string) {
	if h == nil {
		return
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	// If Vary is "*", it already varies on everything.
	for _, v := range h.Values("Vary") {
		if strings.TrimSpace(v) == "*" {
			return
		}
	}

	// Collect existing tokens.
	existing := make(map[string]struct{})
	for _, raw := range h.Values("Vary") {
		for _, tok := range strings.Split(raw, ",") {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}
			existing[strings.ToLower(tok)] = struct{}{}
		}
	}
	if _, ok := existing[strings.ToLower(value)]; ok {
		return
	}

	// Append as a separate header value (keeps formatting simple).
	h.Add("Vary", value)
}

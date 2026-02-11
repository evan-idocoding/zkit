// Package httpx provides small, stable and standard-library flavored net/http helpers.
//
// This package focuses on net/http handler composition (middleware chains). It intentionally
// does not provide a router.
//
// # Middleware chain
//
// A middleware is a standard net/http wrapper:
//
//	type Middleware func(http.Handler) http.Handler
//
// The chain builder is just a slice:
//
//	type Middlewares []Middleware
//
// Order:
//   - Chain(a, b, c).Handler(h) returns a(b(c(h))).
//
// Behavior:
//   - Nil middlewares are ignored.
//   - Handler(nil) and Wrap(nil, ...) panic: nil endpoint is an assembly/config error.
//
// # Example: build a chain and apply to a handler
//
//	base := httpx.Chain(mwRecover, mwRequestID, mwAccessLog)
//	h := base.Handler(finalHandler) // == mwRecover(mwRequestID(mwAccessLog(finalHandler)))
//
// # Example: derive sub-chains
//
//	base := httpx.Chain(mwRecover, mwRequestID, mwAccessLog)
//	admin := base.With(mwAdminAuth)    // base + admin auth
//	download := base.With(mwBodyLimit) // base + body limit
//
// With never mutates the receiver; it returns a new derived chain.
//
// # Positioning note
//
// httpx (its chain and built-in middlewares) primarily serves zkit's admin/ops surfaces. You can
// ignore it entirely and use your existing middleware stack and router (chi, gorilla/mux, etc.).
//
// If you prefer a standard-library-only toolset with explicit, low-magic semantics, httpx is also
// usable in your own business HTTP serving without introducing external dependencies.
//
// # Apply to net/http ServeMux (no router required)
//
//	mux := http.NewServeMux()
//	mux.Handle("/-/healthz", healthHandler)
//	mux.Handle("/api/", apiHandler)
//
//	base := httpx.Chain(mwRecover, mwRequestID, mwAccessLog)
//	root := base.Handler(mux) // wrap the whole mux
//
// # Observability: ChainHandler
//
// Middlewares.Handler / HandlerFunc returns a *httpx.ChainHandler, which keeps:
//
//   - Endpoint: the final handler
//
//   - Middlewares: the (nil-filtered) middleware snapshot used to build the chain
//
//     ch := httpx.Chain(mwRecover).Handler(finalHandler).(*httpx.ChainHandler)
//     _ = ch.Endpoint
//     _ = ch.Middlewares
//
// # Built-in middlewares
//
//   - Recover: recover panics and report (stderr by default).
//   - RequestID: propagate/generate X-Request-ID and store it in context.
//   - RealIP: extract client IP from trusted proxy headers (default-safe).
//   - AccessGuard: allow/deny requests by token/IP/custom check (fail-closed defaults).
//   - Timeout: derive request context with deadline (does not write response).
//   - BodyLimit: enforce request body size (early reject on Content-Length + MaxBytesReader).
//   - CORS: write CORS headers and short-circuit preflight (debug-friendly defaults).
//
// # Middleware options quick reference (admin-oriented)
//
// This section summarizes the exported options/parameters for each built-in middleware so that
// assembly layers (such as github.com/evan-idocoding/zkit/admin) can wire them explicitly without missing knobs.
//
// Recover (RecoverOption):
//   - WithOnPanic(PanicHandler): override panic reporting (default: stderr).
//
// RequestID (RequestIDOption):
//   - WithIncomingHeaders([]string): header priority for incoming id (default: ["X-Request-ID"]).
//   - WithTrustIncoming(bool): whether to accept incoming ids (default: true).
//   - WithSetResponseHeader(bool): whether to set X-Request-ID on the response (default: true).
//   - WithMaxLen(int): max allowed length for incoming id (default: 128).
//   - WithValidator(func(string) bool): validate incoming id (default: conservative [A-Za-z0-9._-] + length).
//   - WithGenerator(RequestIDGenerator): custom generator (fallback to internal generator).
//
// Helpers:
//   - RequestIDFromRequest / RequestIDFromContext
//   - WithRequestID
//
// RealIP (RealIPOption):
//   - WithTrustedProxies([]string): trusted proxy CIDRs/IPs; without it, headers are ignored (default-safe).
//   - WithTrustedHeaders([]string): header priority (default: ["X-Forwarded-For", "X-Real-IP"]).
//   - WithXFFInvalidPolicy(XFFInvalidPolicy): how XFF scanning handles invalid tokens (default: stop).
//
// Helpers:
//   - ParseTrustedProxies (strict parse; returns partial result + error)
//   - RealIPFromRequest / RealIPFromContext
//   - WithRealIP
//
// AccessGuard (AccessGuardOption):
// Token branch (optional):
//   - WithTokenHeader(string): token header name (default: "X-Access-Token").
//   - WithTokens([]string): static token allowlist (empty => deny-all, fail-closed).
//   - WithTokenSet(TokenSetLike): hot-update token set.
//   - WithTokenCheck(func(string) bool): fully custom token predicate.
//
// IP branch (optional):
//   - WithIPAllowList([]string): static IP allowlist (empty => deny-all, fail-closed).
//   - WithIPAllowSet(IPAllowSetLike): hot-update allow set.
//   - WithIPResolver(func(*http.Request) (net.IP, bool)): override IP extraction
//     (default: RealIPFromRequest when present, else RemoteAddr).
//
// Composition / hooks:
//   - WithOr(): combine token+IP with OR instead of default AND.
//   - WithCheck(func(*http.Request) bool): exclusive custom validator (cannot combine with token/IP options).
//   - WithDenyStatus(int): override deny HTTP status (default: 403).
//   - WithOnDeny(func(*http.Request, DenyReason)): observability hook on deny (must not write response).
//
// Helper types (for hot updates):
//   - AtomicTokenSet (implements TokenSetLike)
//   - AtomicIPAllowList (implements IPAllowSetLike)
//
// Timeout (TimeoutOption):
//   - Timeout(timeout time.Duration, ...): base timeout parameter; <= 0 means "skip".
//   - WithTimeoutFunc(TimeoutFunc): per-request timeout decision (can skip).
//   - WithOnTimeout(TimeoutHandler): observability hook when derived context ends with DeadlineExceeded.
//   - WithNow(func() time.Time): custom clock (tests).
//
// BodyLimit (BodyLimitOption):
//   - BodyLimit(maxBytes int64, ...): base limit; <= 0 means "skip".
//   - WithLimitFunc(BodyLimitFunc): per-request limit decision (can skip).
//   - WithOnReject(BodyLimitHandler): observability hook on early reject (Content-Length) or
//     read-time exceed (*http.MaxBytesError).
//
// CORS (CORSOption):
//   - WithEnabledFunc(func(*http.Request) bool): per-request enable toggle (skip entirely when false).
//   - WithMatchFunc(func(*http.Request) bool): per-request matcher (skip entirely when false).
//   - WithAllowCredentials(bool): default true.
//   - WithAllowNullOrigin(bool): default false when allowlist configured; Origin:"null" handling.
//   - WithMaxAge(time.Duration): preflight Access-Control-Max-Age (default 10m; <=0 disables header).
//   - WithPreflightStatus(int): 200 or 204 (default 204).
//   - WithExposeHeaders([]string) / WithExposeHeadersAppend([]string): default exposes X-Request-ID.
//   - WithAllowedMethods([]string): restrict methods (invalid non-empty config => deny-all).
//   - WithAllowedHeaders([]string): restrict request headers for preflight (invalid non-empty config => deny-all).
//   - WithAllowedOrigins([]string): origin hostname allowlist (invalid non-empty config => deny-all).
//
// Helpers:
//   - ValidateOriginPatterns / CountValidOriginPatterns
package httpx

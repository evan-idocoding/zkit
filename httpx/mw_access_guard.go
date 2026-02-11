// AccessGuard middleware.
//
// AccessGuard denies requests unless they pass configured checks. It supports:
//   - token validation (from a header, default: X-Access-Token)
//   - client IP allowlist (RealIP middleware when present; otherwise RemoteAddr)
//   - a fully custom WithCheck predicate (exclusive)
//
// Security defaults are fail-closed: enabling token/IP validation with an empty set denies all.
//
// Minimal usage (static token allowlist):
//
//	h := httpx.Wrap(finalHandler, httpx.AccessGuard(httpx.WithTokens([]string{"t1", "t2"})))
//
// Minimal usage (IP allowlist):
//
//	h := httpx.Wrap(finalHandler, httpx.AccessGuard(httpx.WithIPAllowList([]string{"10.0.0.0/8"})))
package httpx

import (
	"net"
	"net/http"
	"strings"
)

// DefaultAccessGuardTokenHeader is the default header name for AccessGuard token validation.
const DefaultAccessGuardTokenHeader = "X-Access-Token"

// AccessGuardOption configures the AccessGuard middleware.
type AccessGuardOption func(*accessGuardConfig)

type accessGuardConfig struct {
	denyStatus int
	logic      guardLogic

	tokenHeader string
	tokenV      tokenValidator

	ipResolver func(r *http.Request) (net.IP, bool)
	ipV        ipValidator

	check  func(r *http.Request) bool
	onDeny func(r *http.Request, reason DenyReason)

	// Assembly tracking (used to detect conflicting options).
	haveTokenV bool
	haveIPV    bool
	haveCheck  bool
}

type guardLogic int

const (
	guardLogicAll guardLogic = iota // AND (default)
	guardLogicAny                   // OR
)

// DenyReason describes why AccessGuard denied a request.
//
// It is intended for observability only (metrics/logs). It must NOT include
// sensitive values (e.g., tokens, raw header contents).
type DenyReason string

const (
	DenyReasonTokenMissing      DenyReason = "token-missing"
	DenyReasonTokenAmbiguous    DenyReason = "token-ambiguous"
	DenyReasonTokenEmpty        DenyReason = "token-empty"
	DenyReasonTokenSetEmpty     DenyReason = "token-set-empty"
	DenyReasonTokenNotAllowed   DenyReason = "token-not-allowed"
	DenyReasonIPParseFailed     DenyReason = "ip-parse-failed"
	DenyReasonIPAllowListEmpty  DenyReason = "ip-allowlist-empty"
	DenyReasonIPNotAllowed      DenyReason = "ip-not-allowed"
	DenyReasonCustomCheckDenied DenyReason = "check-denied"
)

type tokenValidator interface {
	Validate(token string) (ok bool, reason DenyReason)
}

type ipValidator interface {
	Validate(ip net.IP) (ok bool, reason DenyReason)
}

// WithTokenHeader sets the header name used for token validation.
//
// Default is DefaultAccessGuardTokenHeader. Empty/blank names are ignored.
func WithTokenHeader(name string) AccessGuardOption {
	return func(c *accessGuardConfig) {
		name = strings.TrimSpace(name)
		if name != "" {
			c.tokenHeader = name
		}
	}
}

// WithTokens enables token validation with a static token set.
//
// Semantics:
//   - tokens == nil: enabled, but deny-all (fail-closed)
//   - len(tokens) == 0: enabled, but deny-all (fail-closed)
//   - blank/whitespace tokens are ignored; if none remain, deny-all (fail-closed)
//
// To disable token validation, do NOT configure any token-related option.
func WithTokens(tokens []string) AccessGuardOption {
	return func(c *accessGuardConfig) {
		ensureNoCheck(c, "WithTokens")
		ensureNoTokenV(c, "WithTokens")
		set := NewAtomicTokenSet()
		set.Update(tokens)
		c.tokenV = tokenSetValidator{set: set}
		c.haveTokenV = true
	}
}

// WithTokenSet enables token validation with a user-provided token set.
//
// set must be non-nil. To disable token validation, do NOT configure any token-related option.
func WithTokenSet(set TokenSetLike) AccessGuardOption {
	return func(c *accessGuardConfig) {
		ensureNoCheck(c, "WithTokenSet")
		ensureNoTokenV(c, "WithTokenSet")
		if set == nil {
			panic("httpx: AccessGuard WithTokenSet: nil token set")
		}
		c.tokenV = tokenSetValidator{set: set}
		c.haveTokenV = true
	}
}

// WithTokenCheck enables token validation with a fully custom checker.
//
// fn must be fast and must not block; it must not do I/O.
// If fn is nil, the option is ignored.
func WithTokenCheck(fn func(token string) bool) AccessGuardOption {
	return func(c *accessGuardConfig) {
		if fn == nil {
			return
		}
		ensureNoCheck(c, "WithTokenCheck")
		ensureNoTokenV(c, "WithTokenCheck")
		c.tokenV = tokenCheckValidator{fn: fn}
		c.haveTokenV = true
	}
}

// WithIPAllowList enables IP validation with a static allowlist.
//
// Entries may be CIDRs (e.g. "10.0.0.0/8", "fd00::/8") or single IPs
// (e.g. "192.168.1.1", "2001:db8::1"), where single IPs are treated as /32 or /128.
//
// Semantics:
//   - cidrsOrIPs == nil: enabled, but deny-all (fail-closed)
//   - len(cidrsOrIPs) == 0: enabled, but deny-all (fail-closed)
//   - invalid entries are ignored; if no valid entries remain, deny-all (fail-closed)
//
// To disable IP validation, do NOT configure any IP-related option.
func WithIPAllowList(cidrsOrIPs []string) AccessGuardOption {
	return func(c *accessGuardConfig) {
		ensureNoCheck(c, "WithIPAllowList")
		ensureNoIPV(c, "WithIPAllowList")
		allow := NewAtomicIPAllowList()
		allow.Update(cidrsOrIPs)
		c.ipV = ipAllowSetValidator{set: allow}
		c.haveIPV = true
	}
}

// WithIPAllowSet enables IP validation with a user-provided allow set.
//
// set must be non-nil. To disable IP validation, do NOT configure any IP-related option.
func WithIPAllowSet(set IPAllowSetLike) AccessGuardOption {
	return func(c *accessGuardConfig) {
		ensureNoCheck(c, "WithIPAllowSet")
		ensureNoIPV(c, "WithIPAllowSet")
		if set == nil {
			panic("httpx: AccessGuard WithIPAllowSet: nil allow set")
		}
		c.ipV = ipAllowSetValidator{set: set}
		c.haveIPV = true
	}
}

// WithIPResolver sets a custom IP resolver.
//
// Default is:
//   - RealIPFromRequest(r) when present
//   - otherwise parseIP(r.RemoteAddr)
//
// If fn is nil, the option is ignored.
func WithIPResolver(fn func(r *http.Request) (net.IP, bool)) AccessGuardOption {
	return func(c *accessGuardConfig) {
		if fn != nil {
			c.ipResolver = fn
		}
	}
}

// WithDenyStatus sets the HTTP status code used for denied requests.
//
// Default is 403 (Forbidden). If code <= 0, it leaves the default unchanged.
func WithDenyStatus(code int) AccessGuardOption {
	return func(c *accessGuardConfig) {
		if code > 0 {
			c.denyStatus = code
		}
	}
}

// WithOr switches the combination logic from the default AND to OR.
//
// When both token and IP are enabled:
//   - default (AND): ok = tokenOK && ipOK
//   - WithOr (OR):  ok = tokenOK || ipOK
func WithOr() AccessGuardOption {
	return func(c *accessGuardConfig) {
		ensureNoCheck(c, "WithOr")
		c.logic = guardLogicAny
	}
}

// WithCheck sets a fully custom single-step validator.
//
// Strong semantics:
//   - If WithCheck is set, it MUST be the only validation mechanism.
//     Combining it with token/IP options is an assembly/config error (panic).
//   - fn must be fast and must not block; it must not do I/O.
func WithCheck(fn func(r *http.Request) bool) AccessGuardOption {
	return func(c *accessGuardConfig) {
		if fn == nil {
			return
		}
		if c.logic == guardLogicAny {
			panic("httpx: AccessGuard WithCheck conflicts with WithOr")
		}
		if c.haveTokenV || c.haveIPV {
			panic("httpx: AccessGuard WithCheck conflicts with token/ip options")
		}
		c.check = fn
		c.haveCheck = true
	}
}

// WithOnDeny sets a hook called when AccessGuard denies a request.
//
// Observability-only: implementations must be fast and must not panic.
// Implementations must NOT write the response.
// If the hook panics, AccessGuard will swallow the panic and report it to stderr
// (style aligned with Timeout / BodyLimit).
func WithOnDeny(fn func(r *http.Request, reason DenyReason)) AccessGuardOption {
	return func(c *accessGuardConfig) {
		if fn != nil {
			c.onDeny = fn
		}
	}
}

// AccessGuard returns a middleware that enforces an access guard based on:
//   - optional token validation (token from a header + validator)
//   - optional client IP allowlist (real IP from RealIP middleware when present)
//
// Rules:
//   - If IP allowlist is enabled, the client IP must match one of the allowlisted CIDRs/IPs.
//     Client IP is taken from RealIP middleware when present; otherwise it falls back to RemoteAddr.
//   - If token validation is enabled, the request must provide exactly one non-empty token header value
//     that matches the configured token validator.
//   - If both are enabled, both checks must pass (AND).
//   - If neither is enabled, it panics (configuration/assembly error).
//
// Security defaults:
//   - When token validation is enabled but the token set is empty, it denies all (fail-closed).
//   - When IP validation is enabled but the allowlist is empty, it denies all (fail-closed).
//   - OR must be explicitly enabled via WithOr().
func AccessGuard(opts ...AccessGuardOption) Middleware {
	cfg := accessGuardConfig{
		denyStatus:  http.StatusForbidden,
		logic:       guardLogicAll,
		tokenHeader: DefaultAccessGuardTokenHeader,
		ipResolver:  defaultAccessGuardIPResolver,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if strings.TrimSpace(cfg.tokenHeader) == "" {
		cfg.tokenHeader = DefaultAccessGuardTokenHeader
	}
	if cfg.denyStatus == 0 {
		cfg.denyStatus = http.StatusForbidden
	}

	if cfg.haveCheck {
		if cfg.haveTokenV || cfg.haveIPV {
			panic("httpx: AccessGuard WithCheck conflicts with token/ip options")
		}
	}

	tokenEnabled := cfg.tokenV != nil
	ipEnabled := cfg.ipV != nil
	checkEnabled := cfg.check != nil
	if !tokenEnabled && !ipEnabled && !checkEnabled {
		panic("httpx: access_guard has no checks configured")
	}

	return func(next http.Handler) http.Handler {
		if next == nil {
			panic("httpx: nil next handler")
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r == nil {
				panic("httpx: nil request")
			}

			if checkEnabled {
				if !cfg.check(r) {
					accessGuardDeny(w, r, cfg.denyStatus, cfg.onDeny, DenyReasonCustomCheckDenied)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			var (
				tokenOK  = true
				ipOK     = true
				tokenWhy DenyReason
				ipWhy    DenyReason
			)
			if tokenEnabled {
				tokenOK, tokenWhy = accessGuardTokenOK(r, cfg.tokenHeader, cfg.tokenV)
			}
			if ipEnabled {
				ipOK, ipWhy = accessGuardIPOK(r, cfg.ipResolver, cfg.ipV)
			}
			ok := accessGuardCombine(cfg.logic, tokenEnabled, tokenOK, ipEnabled, ipOK)
			if !ok {
				// Prefer reporting the first enabled branch's reason, for stability.
				reason := DenyReasonCustomCheckDenied
				if tokenEnabled && !tokenOK {
					reason = tokenWhy
				} else if ipEnabled && !ipOK {
					reason = ipWhy
				}
				accessGuardDeny(w, r, cfg.denyStatus, cfg.onDeny, reason)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func accessGuardCombine(logic guardLogic, tokenEnabled bool, tokenOK bool, ipEnabled bool, ipOK bool) bool {
	switch logic {
	case guardLogicAny:
		// OR mode: if both enabled => tokenOK || ipOK; if only one enabled => that one.
		if tokenEnabled && ipEnabled {
			return tokenOK || ipOK
		}
		if tokenEnabled {
			return tokenOK
		}
		return ipOK
	default:
		// AND mode (default): if both enabled => tokenOK && ipOK; if only one enabled => that one.
		if tokenEnabled && ipEnabled {
			return tokenOK && ipOK
		}
		if tokenEnabled {
			return tokenOK
		}
		return ipOK
	}
}

func accessGuardTokenOK(r *http.Request, header string, v tokenValidator) (ok bool, reason DenyReason) {
	raw, why, ok := singleHeaderValueWithReason(r.Header, header)
	if !ok {
		return false, why
	}
	token := strings.TrimSpace(raw)
	if token == "" {
		return false, DenyReasonTokenEmpty
	}
	return v.Validate(token)
}

func accessGuardIPOK(r *http.Request, resolver func(*http.Request) (net.IP, bool), v ipValidator) (ok bool, reason DenyReason) {
	ip, ok := resolver(r)
	if !ok || ip == nil {
		return false, DenyReasonIPParseFailed
	}
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	return v.Validate(ip)
}

func defaultAccessGuardIPResolver(r *http.Request) (net.IP, bool) {
	if rip, ok := RealIPFromRequest(r); ok && rip != nil {
		return rip, true
	}
	ip := parseIP(r.RemoteAddr)
	if ip == nil {
		return nil, false
	}
	return ip, true
}

func singleHeaderValueWithReason(h http.Header, name string) (value string, reason DenyReason, ok bool) {
	if h == nil {
		return "", DenyReasonTokenMissing, false
	}
	vs := h.Values(name)
	if len(vs) == 0 {
		return "", DenyReasonTokenMissing, false
	}
	if len(vs) != 1 {
		return "", DenyReasonTokenAmbiguous, false
	}
	return vs[0], "", true
}

func accessGuardDeny(w http.ResponseWriter, r *http.Request, status int, onDeny func(*http.Request, DenyReason), reason DenyReason) {
	if onDeny != nil {
		if p := callOnDenyNoPanic(onDeny, r, reason); p != nil {
			reportAccessGuardHookPanicToStderr(r, p)
		}
	}
	w.WriteHeader(status)
}

func callOnDenyNoPanic(fn func(*http.Request, DenyReason), r *http.Request, reason DenyReason) (panicked any) {
	defer func() {
		if p := recover(); p != nil {
			panicked = p
		}
	}()
	fn(r, reason)
	return nil
}

func ensureNoTokenV(c *accessGuardConfig, opt string) {
	if c.haveTokenV {
		panic("httpx: AccessGuard " + opt + " conflicts with existing token option")
	}
}

func ensureNoIPV(c *accessGuardConfig, opt string) {
	if c.haveIPV {
		panic("httpx: AccessGuard " + opt + " conflicts with existing IP option")
	}
}

func ensureNoCheck(c *accessGuardConfig, opt string) {
	if c.haveCheck {
		panic("httpx: AccessGuard " + opt + " conflicts with WithCheck")
	}
}

package admin

import (
	"net/http"
	"strings"

	"github.com/evan-idocoding/zkit/httpx"
)

// Guard enforces request admission for a capability.
//
// Implementations must be fast and must not block; they must not do I/O.
type Guard interface {
	// Middleware returns a net/http middleware that enforces this guard.
	//
	// Denied requests must respond with HTTP 403.
	Middleware() func(http.Handler) http.Handler
}

type guardFunc struct{ mw httpx.Middleware }

func (g guardFunc) Middleware() func(http.Handler) http.Handler {
	if g.mw == nil {
		// Treat nil middleware as deny-all (conservative).
		return DenyAll().Middleware()
	}
	return g.mw
}

// DenyAll returns a guard that denies all requests with HTTP 403.
func DenyAll() Guard {
	return guardFunc{mw: func(next http.Handler) http.Handler {
		if next == nil {
			panic("admin: DenyAll: nil next handler")
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		})
	}}
}

// AllowAll returns a guard that allows all requests.
func AllowAll() Guard {
	return guardFunc{mw: func(next http.Handler) http.Handler {
		if next == nil {
			panic("admin: AllowAll: nil next handler")
		}
		return next
	}}
}

// DefaultTokenHeader is the default header used by token-based guards when not overridden.
const DefaultTokenHeader = "X-Access-Token"

// TokenSetLike is a token set used by token-based guards.
//
// Implementations must be safe for concurrent use.
// The request path must be fast and must not block.
type TokenSetLike interface {
	Contains(token string) bool
}

type TokenOption func(*tokenConfig)

type tokenConfig struct {
	header string
}

// WithTokenHeader overrides the token header name for token-based guards.
//
// Empty/blank names are ignored (default is DefaultTokenHeader).
func WithTokenHeader(name string) TokenOption {
	return func(c *tokenConfig) {
		if c == nil {
			return
		}
		name = strings.TrimSpace(name)
		if name != "" {
			c.header = name
		}
	}
}

func applyTokenOptions(opts []TokenOption) tokenConfig {
	cfg := tokenConfig{header: DefaultTokenHeader}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if strings.TrimSpace(cfg.header) == "" {
		cfg.header = DefaultTokenHeader
	}
	return cfg
}

// Tokens returns a guard that validates requests using a static token list.
//
// Semantics are inherited from httpx.AccessGuard:
//   - nil/empty tokens => deny-all (fail-closed)
//   - blank tokens are ignored; if none remain => deny-all
func Tokens(tokens []string, opts ...TokenOption) Guard {
	cfg := applyTokenOptions(opts)
	return guardFunc{mw: httpx.AccessGuard(
		httpx.WithTokenHeader(cfg.header),
		httpx.WithTokens(tokens),
	)}
}

// HotTokens returns a guard that validates requests using a hot-update token set.
//
// set must be non-nil (nil is an assembly error and will panic).
func HotTokens(set TokenSetLike, opts ...TokenOption) Guard {
	if set == nil {
		panic("admin: HotTokens: nil token set")
	}
	cfg := applyTokenOptions(opts)
	return guardFunc{mw: httpx.AccessGuard(
		httpx.WithTokenHeader(cfg.header),
		httpx.WithTokenSet(set),
	)}
}

// IPAllowList returns a guard backed by a static IP allowlist.
//
// Entries may be CIDRs or single IPs. Empty/invalid inputs deny all (fail-closed).
func IPAllowList(cidrsOrIPs ...string) Guard {
	return guardFunc{mw: httpx.AccessGuard(
		httpx.WithIPAllowList(cidrsOrIPs),
	)}
}

// TokensOrIPAllowList returns a guard that allows a request when:
//   - token is allowed, OR
//   - client IP is allowlisted.
//
// This is a thin wrapper around httpx.AccessGuard with WithOr().
func TokensOrIPAllowList(tokens []string, cidrsOrIPs []string, opts ...TokenOption) Guard {
	cfg := applyTokenOptions(opts)
	return guardFunc{mw: httpx.AccessGuard(
		httpx.WithTokenHeader(cfg.header),
		httpx.WithTokens(tokens),
		httpx.WithIPAllowList(cidrsOrIPs),
		httpx.WithOr(),
	)}
}

// HotTokensOrIPAllowList is like TokensOrIPAllowList, but token validation uses a hot-update set.
func HotTokensOrIPAllowList(set TokenSetLike, cidrsOrIPs []string, opts ...TokenOption) Guard {
	if set == nil {
		panic("admin: HotTokensOrIPAllowList: nil token set")
	}
	cfg := applyTokenOptions(opts)
	return guardFunc{mw: httpx.AccessGuard(
		httpx.WithTokenHeader(cfg.header),
		httpx.WithTokenSet(set),
		httpx.WithIPAllowList(cidrsOrIPs),
		httpx.WithOr(),
	)}
}

// TokensAndIPAllowList returns a guard that allows a request when:
//   - token is allowed, AND
//   - client IP is allowlisted.
//
// This is a thin wrapper around httpx.AccessGuard (default AND semantics).
func TokensAndIPAllowList(tokens []string, cidrsOrIPs []string, opts ...TokenOption) Guard {
	cfg := applyTokenOptions(opts)
	return guardFunc{mw: httpx.AccessGuard(
		httpx.WithTokenHeader(cfg.header),
		httpx.WithTokens(tokens),
		httpx.WithIPAllowList(cidrsOrIPs),
	)}
}

// HotTokensAndIPAllowList is like TokensAndIPAllowList, but token validation uses a hot-update set.
func HotTokensAndIPAllowList(set TokenSetLike, cidrsOrIPs []string, opts ...TokenOption) Guard {
	if set == nil {
		panic("admin: HotTokensAndIPAllowList: nil token set")
	}
	cfg := applyTokenOptions(opts)
	return guardFunc{mw: httpx.AccessGuard(
		httpx.WithTokenHeader(cfg.header),
		httpx.WithTokenSet(set),
		httpx.WithIPAllowList(cidrsOrIPs),
	)}
}

// Check returns a guard backed by a custom fast predicate.
//
// fn must be fast and must not block; it must not do I/O.
// fn == nil is an assembly error and will panic.
func Check(fn func(r *http.Request) bool) Guard {
	if fn == nil {
		panic("admin: Check: nil func")
	}
	return guardFunc{mw: httpx.AccessGuard(
		httpx.WithCheck(fn),
	)}
}

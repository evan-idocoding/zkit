// Package zkit re-exports Guard types and constructors from the admin subpackage
// so callers can configure admin without importing admin.
package zkit

import (
	"net/http"

	"github.com/evan-idocoding/zkit/admin"
)

// Guard enforces request admission for admin endpoints.
// Use AllowAll, DenyAll, Tokens, IPAllowList, etc. to create one.
type Guard = admin.Guard

// TokenSetLike is a token set for hot-update guards (e.g. HotTokens).
type TokenSetLike = admin.TokenSetLike

// TokenOption configures token-based guards (e.g. header name).
type TokenOption = admin.TokenOption

// DefaultTokenHeader is the default header name for token-based guards.
const DefaultTokenHeader = admin.DefaultTokenHeader

// AllowAll returns a guard that allows all requests.
func AllowAll() Guard { return admin.AllowAll() }

// DenyAll returns a guard that denies all requests with HTTP 403.
func DenyAll() Guard { return admin.DenyAll() }

// WithTokenHeader overrides the token header name for token-based guards.
func WithTokenHeader(name string) TokenOption { return admin.WithTokenHeader(name) }

// Tokens returns a guard that validates requests using a static token list.
func Tokens(tokens []string, opts ...TokenOption) Guard {
	return admin.Tokens(tokens, opts...)
}

// HotTokens returns a guard that validates requests using a hot-update token set.
func HotTokens(set TokenSetLike, opts ...TokenOption) Guard {
	return admin.HotTokens(set, opts...)
}

// IPAllowList returns a guard backed by a static IP allowlist (CIDRs or single IPs).
func IPAllowList(cidrsOrIPs ...string) Guard {
	return admin.IPAllowList(cidrsOrIPs...)
}

// TokensOrIPAllowList returns a guard that allows when token is allowed OR IP is allowlisted.
func TokensOrIPAllowList(tokens []string, cidrsOrIPs []string, opts ...TokenOption) Guard {
	return admin.TokensOrIPAllowList(tokens, cidrsOrIPs, opts...)
}

// HotTokensOrIPAllowList is like TokensOrIPAllowList but with a hot-update token set.
func HotTokensOrIPAllowList(set TokenSetLike, cidrsOrIPs []string, opts ...TokenOption) Guard {
	return admin.HotTokensOrIPAllowList(set, cidrsOrIPs, opts...)
}

// TokensAndIPAllowList returns a guard that allows when token is allowed AND IP is allowlisted.
func TokensAndIPAllowList(tokens []string, cidrsOrIPs []string, opts ...TokenOption) Guard {
	return admin.TokensAndIPAllowList(tokens, cidrsOrIPs, opts...)
}

// HotTokensAndIPAllowList is like TokensAndIPAllowList but with a hot-update token set.
func HotTokensAndIPAllowList(set TokenSetLike, cidrsOrIPs []string, opts ...TokenOption) Guard {
	return admin.HotTokensAndIPAllowList(set, cidrsOrIPs, opts...)
}

// Check returns a guard backed by a custom fast predicate (must not block, no I/O).
func Check(fn func(r *http.Request) bool) Guard {
	return admin.Check(fn)
}

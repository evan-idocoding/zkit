package admin

import (
	"net/http"

	"github.com/evan-idocoding/zkit/httpx"
)

// New assembles and returns the admin subtree handler.
//
// Security & control:
//   - Nothing is mounted unless explicitly enabled via options.
//   - Every enabled capability must have a non-nil Guard (explicit).
//
// Assembly errors are fail-fast and will panic.
func New(opts ...Option) http.Handler {
	b := newBuilder()
	for _, opt := range opts {
		if opt != nil {
			opt(b)
		}
	}
	return b.build()
}

// Option configures admin assembly.
type Option func(*Builder)

// RealIPSpec configures how admin extracts client IP for IP-based guards.
//
// Default-safe: if TrustedProxies is empty, headers are not trusted and RemoteAddr is used.
type RealIPSpec struct {
	// TrustedProxies declares which direct client IP ranges are trusted proxies.
	// Accepts CIDRs or single IPs (e.g. "10.0.0.0/8", "192.168.1.1").
	TrustedProxies []string

	// TrustedHeaders optionally overrides header priority. If empty, admin uses the
	// standard order:
	//   - X-Forwarded-For
	//   - X-Real-IP
	TrustedHeaders []string
}

// WithRealIP sets real IP extraction config for the admin subtree.
//
// This affects IP-based guards (IPAllowList and token+IP composite guards).
func WithRealIP(spec RealIPSpec) Option {
	return func(b *Builder) {
		if b == nil {
			return
		}
		b.realIP = spec
	}
}

// Builder collects capabilities and builds the final admin handler.
//
// It is intentionally not exposed; users configure admin via Options.
type Builder struct {
	realIP RealIPSpec

	paths map[string]http.Handler // path -> handler (one capability per path)

	// Late-assembled endpoint.
	report *ReportSpec

	// Data sources for /report (captured at assembly time when endpoints are enabled).
	reportState reportState
}

func newBuilder() *Builder {
	return &Builder{
		paths: make(map[string]http.Handler),
	}
}

func (b *Builder) build() http.Handler {
	// Late-assembled endpoints depend on what was enabled.
	b.assembleReport()

	mux := http.NewServeMux()

	// Mount all registered paths.
	for path, h := range b.paths {
		if path == "" || h == nil {
			continue
		}
		mux.Handle(path, h)
	}

	// Global admin subtree chain (implementation detail).
	// Keep it conservative: Recover + RequestID + RealIP (default-safe).
	chain := httpx.Chain(
		httpx.Recover(),
		httpx.RequestID(),
	)

	// RealIP is always placed before any IP-based guard.
	//
	// admin includes it unconditionally; it is default-safe when TrustedProxies is empty.
	realIPMW := httpx.RealIP(
		httpx.WithTrustedProxies(b.realIP.TrustedProxies),
		httpx.WithTrustedHeaders(b.realIP.TrustedHeaders),
	)
	chain = chain.With(realIPMW)

	return chain.Handler(mux)
}

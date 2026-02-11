// RealIP middleware.
//
// RealIP extracts the real client IP address and stores it in the request context for downstream
// handlers (RealIPFromRequest).
//
// Security model:
//   - Default-safe: without trusted proxies configured, it ignores proxy headers and uses RemoteAddr.
//   - When trusted proxies are configured, it trusts headers only when the direct client IP is trusted.
//
// Minimal usage (behind a known load balancer):
//
//	h := httpx.Wrap(finalHandler,
//		httpx.RealIP(httpx.WithTrustedProxies([]string{"10.0.0.0/8"})),
//	)
package httpx

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// Default headers checked for real IP, in order of priority.
var defaultRealIPHeaders = []string{"X-Forwarded-For", "X-Real-IP"}

// XFFInvalidPolicy controls how X-Forwarded-For parsing handles invalid entries.
//
// When scanning XFF right-to-left, an "invalid entry" is any token that cannot be parsed
// as an IP (optionally with a port).
type XFFInvalidPolicy int

const (
	// XFFInvalidStop stops processing and returns nil immediately when an invalid entry
	// is encountered. This is the most conservative behavior and the default.
	XFFInvalidStop XFFInvalidPolicy = iota
	// XFFInvalidSkip skips invalid entries and continues scanning left.
	// This is more compatible with real-world chains that may include garbage tokens,
	// but it is less strict.
	XFFInvalidSkip
	// XFFInvalidSkipUnknown skips entries equal to "unknown" (case-insensitive),
	// but still stops on other invalid values.
	XFFInvalidSkipUnknown
)

// RealIPOption configures the RealIP middleware.
type RealIPOption func(*realIPConfig)

type realIPConfig struct {
	// trustedProxies is the list of trusted proxy CIDRs.
	// Only requests from these CIDRs will have their headers trusted.
	trustedProxies []*net.IPNet

	// headers are checked in order for the real IP.
	headers []string

	// xffInvalidPolicy controls how XFF parsing handles invalid entries.
	xffInvalidPolicy XFFInvalidPolicy
}

// ParseTrustedProxies parses trusted proxy CIDRs/IPs.
//
// It accepts CIDR notation (e.g., "10.0.0.0/8") or single IPs (e.g., "192.168.1.1").
// Single IPs are treated as /32 (IPv4) or /128 (IPv6).
//
// It returns the successfully parsed nets and an error if any entries were invalid.
// Empty/blank entries are ignored.
func ParseTrustedProxies(cidrs []string) ([]*net.IPNet, error) {
	if len(cidrs) == 0 {
		return nil, nil
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	invalid := make([]string, 0)
	for _, raw := range cidrs {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		_, ipNet, err := net.ParseCIDR(s)
		if err != nil {
			// Try parsing as single IP.
			ip := net.ParseIP(s)
			if ip == nil {
				invalid = append(invalid, fmt.Sprintf("%q", raw))
				continue
			}
			// Normalize and convert to CIDR.
			// Use To4() to get 4-byte representation for IPv4, ensuring
			// consistent matching with normalized IPs in isTrustedIP.
			if ip4 := ip.To4(); ip4 != nil {
				ip = ip4
				ipNet = &net.IPNet{IP: ip, Mask: net.CIDRMask(32, 32)}
			} else {
				ipNet = &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}
			}
		}
		nets = append(nets, ipNet)
	}
	if len(invalid) > 0 {
		return nets, fmt.Errorf("httpx: invalid trusted proxy CIDR/IP entries: %s", strings.Join(invalid, ", "))
	}
	return nets, nil
}

// WithTrustedProxies sets the list of trusted proxy CIDRs.
//
// Only when the direct client IP (from RemoteAddr) is within one of these CIDRs,
// the middleware will trust and extract IP from headers.
//
// Accepts CIDR notation (e.g., "10.0.0.0/8") or single IPs (e.g., "192.168.1.1").
// Single IPs are treated as /32 (IPv4) or /128 (IPv6).
// Invalid entries are silently ignored.
//
// For strict validation (e.g., fail-fast at startup), use ParseTrustedProxies.
func WithTrustedProxies(cidrs []string) RealIPOption {
	return func(c *realIPConfig) {
		if len(cidrs) == 0 {
			c.trustedProxies = nil
			return
		}
		nets, _ := ParseTrustedProxies(cidrs)
		c.trustedProxies = nets
	}
}

// WithTrustedHeaders sets the list of header names to check for the real IP, in order.
//
// Default is ["X-Forwarded-For", "X-Real-IP"].
// Empty/blank names are ignored. If the resulting list is empty, it falls back to the default.
func WithTrustedHeaders(headers []string) RealIPOption {
	return func(c *realIPConfig) {
		if len(headers) == 0 {
			c.headers = nil
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
		c.headers = out
	}
}

// WithXFFInvalidPolicy sets how X-Forwarded-For parsing handles invalid entries.
//
// Default is XFFInvalidStop (most conservative).
func WithXFFInvalidPolicy(p XFFInvalidPolicy) RealIPOption {
	return func(c *realIPConfig) {
		switch p {
		case XFFInvalidStop, XFFInvalidSkip, XFFInvalidSkipUnknown:
			c.xffInvalidPolicy = p
		default:
			c.xffInvalidPolicy = XFFInvalidStop
		}
	}
}

// RealIP returns a middleware that extracts the real client IP from the request.
//
// Behavior:
//   - By default, it does NOT trust any headers and uses RemoteAddr directly.
//   - If trusted proxies are configured (via WithTrustedProxies), and the direct client IP
//     is within a trusted CIDR, it extracts the real IP from headers.
//   - For X-Forwarded-For, it scans right-to-left, skipping trusted proxies, and returns
//     the first untrusted IP. If an invalid entry is encountered, behavior depends on
//     WithXFFInvalidPolicy (default: XFFInvalidStop).
//   - For X-Real-IP (and other single-value headers), it uses the value only when the header
//     has exactly one non-empty value; multiple values are treated as invalid/ambiguous.
//   - The extracted IP is stored in the request context for downstream handlers. If no IP
//     can be extracted (e.g., unparseable RemoteAddr), the context is left unchanged.
//
// Security:
//   - Default-safe: without trusted proxies configured, headers are ignored.
//   - Only configure trusted proxies for IPs you actually trust (e.g., your load balancer).
func RealIP(opts ...RealIPOption) Middleware {
	cfg := realIPConfig{
		headers:          defaultRealIPHeaders,
		xffInvalidPolicy: XFFInvalidStop,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if len(cfg.headers) == 0 {
		cfg.headers = defaultRealIPHeaders
	}

	return func(next http.Handler) http.Handler {
		if next == nil {
			panic("httpx: nil next handler")
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r == nil {
				panic("httpx: nil request")
			}

			realIP := extractRealIP(r, cfg.trustedProxies, cfg.headers, cfg.xffInvalidPolicy)
			ctx := WithRealIP(r.Context(), realIP)
			if ctx == r.Context() {
				next.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// realIPKey is the context key for storing the real client IP.
type realIPKey struct{}

// RealIPFromContext extracts the real IP from ctx.
//
// Returns nil and false if the context does not contain a real IP
// (e.g., request did not pass through the RealIP middleware).
func RealIPFromContext(ctx context.Context) (net.IP, bool) {
	if ctx == nil {
		return nil, false
	}
	ip, ok := ctx.Value(realIPKey{}).(net.IP)
	if !ok || ip == nil {
		return nil, false
	}
	return ip, true
}

// RealIPFromRequest extracts the real IP from r.Context().
func RealIPFromRequest(r *http.Request) (net.IP, bool) {
	if r == nil {
		return nil, false
	}
	return RealIPFromContext(r.Context())
}

// WithRealIP returns a derived context with ip stored as the real IP.
//
// If ip is nil, it returns ctx unchanged.
func WithRealIP(ctx context.Context, ip net.IP) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if ip == nil {
		return ctx
	}
	return context.WithValue(ctx, realIPKey{}, ip)
}

// extractRealIP extracts the real client IP from the request.
func extractRealIP(r *http.Request, trustedProxies []*net.IPNet, headers []string, xffInvalidPolicy XFFInvalidPolicy) net.IP {
	// Parse direct IP from RemoteAddr.
	directIP := parseIP(r.RemoteAddr)
	if directIP == nil {
		// Cannot parse RemoteAddr, nothing we can do.
		return nil
	}

	// If no trusted proxies configured, don't trust headers.
	if len(trustedProxies) == 0 {
		return directIP
	}

	// Check if direct IP is from a trusted proxy.
	if !isTrustedIP(directIP, trustedProxies) {
		return directIP
	}

	// Direct IP is trusted, try to extract real IP from headers.
	for _, header := range headers {
		ip := extractIPFromHeader(r, header, trustedProxies, xffInvalidPolicy)
		if ip != nil {
			return ip
		}
	}

	// No valid IP found in headers, fall back to direct IP.
	return directIP
}

// extractIPFromHeader extracts IP from a specific header.
func extractIPFromHeader(r *http.Request, header string, trustedProxies []*net.IPNet, xffInvalidPolicy XFFInvalidPolicy) net.IP {
	// HTTP allows multiple headers with the same name; they should be treated
	// as comma-joined per RFC 7230. For XFF, we explicitly join all values; for other
	// headers, multiple values are treated as invalid/ambiguous.
	values := r.Header.Values(header)
	if len(values) == 0 {
		return nil
	}

	// X-Forwarded-For is comma-separated, scan right-to-left.
	if strings.EqualFold(header, "X-Forwarded-For") {
		value := strings.Join(values, ", ")
		return extractFromXFF(value, trustedProxies, xffInvalidPolicy)
	}

	// Other headers (X-Real-IP, etc.) are single values.
	// If multiple header values exist, treat it as invalid/ambiguous.
	if len(values) != 1 {
		return nil
	}
	v := strings.TrimSpace(values[0])
	if v == "" {
		return nil
	}
	return parseIP(v)
}

// extractFromXFF extracts the real IP from X-Forwarded-For header.
//
// It scans right-to-left, skipping trusted proxies, and returns the first untrusted IP.
// If an invalid entry is encountered during scanning, behavior depends on xffInvalidPolicy
// (default: stop and return nil, causing caller to fall back to directIP).
func extractFromXFF(value string, trustedProxies []*net.IPNet, xffInvalidPolicy XFFInvalidPolicy) net.IP {
	parts := strings.Split(value, ",")

	// Scan right-to-left.
	for i := len(parts) - 1; i >= 0; i-- {
		ipStr := strings.TrimSpace(parts[i])
		if ipStr == "" {
			continue
		}
		if xffInvalidPolicy == XFFInvalidSkipUnknown && strings.EqualFold(ipStr, "unknown") {
			continue
		}
		ip := parseIP(ipStr)
		if ip == nil {
			// Invalid entry.
			switch xffInvalidPolicy {
			case XFFInvalidSkip:
				continue
			case XFFInvalidSkipUnknown:
				// Only "unknown" is skipped above; other invalid values stop.
				return nil
			case XFFInvalidStop:
				fallthrough
			default:
				// Stop here (don't trust anything to the left).
				return nil
			}
		}
		if !isTrustedIP(ip, trustedProxies) {
			// Found an untrusted IP, this is the real client.
			return ip
		}
		// This IP is trusted (a proxy), continue to the left.
	}

	// All IPs in the chain are trusted, return nil.
	return nil
}

// isTrustedIP checks if ip is within any of the trusted CIDRs.
func isTrustedIP(ip net.IP, trustedProxies []*net.IPNet) bool {
	// Normalize IPv4-mapped IPv6 addresses (e.g., ::ffff:10.0.0.1 -> 10.0.0.1).
	// This ensures proper matching on dual-stack servers where RemoteAddr
	// might be in IPv6 format even for IPv4 clients.
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	for _, cidr := range trustedProxies {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// parseIP parses an IP address from a string that may include a port.
func parseIP(s string) net.IP {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	// Try to parse as host:port first.
	host, _, err := net.SplitHostPort(s)
	if err == nil {
		s = host
	}

	return net.ParseIP(s)
}

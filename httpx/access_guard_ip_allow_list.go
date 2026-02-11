package httpx

import (
	"net"
	"strings"
	"sync/atomic"
)

// IPAllowSetLike is an IP allow set used by AccessGuard.
//
// Implementations must be safe for concurrent use.
// The request path must be fast and must not block.
type IPAllowSetLike interface {
	Contains(ip net.IP) bool
}

// AtomicIPAllowList is an updateable IP allowlist intended for hot changes.
//
// Read path (Contains) is lock-free and non-blocking.
// Write path (Update/AllowAll) is atomic and may allocate.
type AtomicIPAllowList struct {
	snap atomic.Pointer[ipAllowSnapshot]
}

type ipAllowSnapshot struct {
	allowAll bool
	nets     []*net.IPNet
}

// NewAtomicIPAllowList creates a new allowlist in the deny-all state.
func NewAtomicIPAllowList() *AtomicIPAllowList {
	a := &AtomicIPAllowList{}
	a.snap.Store(&ipAllowSnapshot{})
	return a
}

// Update parses and replaces the current allowlist snapshot.
//
// Semantics:
//   - cidrsOrIPs == nil: sets to empty (deny-all)
//   - entries may be CIDRs or single IPs
//   - invalid/blank entries are ignored
//   - if no valid entries remain, it becomes empty (deny-all)
func (a *AtomicIPAllowList) Update(cidrsOrIPs []string) {
	if a == nil {
		return
	}
	nets := parseCIDRsOrIPs(cidrsOrIPs)
	a.snap.Store(&ipAllowSnapshot{nets: nets})
}

// AllowAll sets this allowlist to allow all IPs.
func (a *AtomicIPAllowList) AllowAll() {
	if a == nil {
		return
	}
	a.snap.Store(&ipAllowSnapshot{allowAll: true})
}

// Contains reports whether ip is allowed.
//
// It is safe for concurrent use.
func (a *AtomicIPAllowList) Contains(ip net.IP) bool {
	if a == nil {
		return false
	}
	snap := a.snap.Load()
	if snap == nil {
		return false
	}
	if snap.allowAll {
		return true
	}
	if len(snap.nets) == 0 {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	for _, n := range snap.nets {
		if n != nil && n.Contains(ip) {
			return true
		}
	}
	return false
}

func (a *AtomicIPAllowList) empty() bool {
	if a == nil {
		return true
	}
	snap := a.snap.Load()
	if snap == nil {
		return true
	}
	return !snap.allowAll && len(snap.nets) == 0
}

type ipAllowSetValidator struct{ set IPAllowSetLike }

type ipAllowSetEmptyAware interface {
	IPAllowSetLike
	empty() bool
}

func (v ipAllowSetValidator) Validate(ip net.IP) (ok bool, reason DenyReason) {
	if v.set == nil {
		return false, DenyReasonIPAllowListEmpty
	}
	if ea, ok := v.set.(ipAllowSetEmptyAware); ok && ea.empty() {
		return false, DenyReasonIPAllowListEmpty
	}
	if v.set.Contains(ip) {
		return true, ""
	}
	return false, DenyReasonIPNotAllowed
}

func parseCIDRsOrIPs(in []string) []*net.IPNet {
	if len(in) == 0 {
		return nil
	}
	out := make([]*net.IPNet, 0, len(in))
	for _, raw := range in {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		_, ipNet, err := net.ParseCIDR(s)
		if err == nil && ipNet != nil {
			// Normalize IPv4 CIDR IP to 4-byte representation for consistent matching.
			if ip4 := ipNet.IP.To4(); ip4 != nil {
				ipNet.IP = ip4
			}
			out = append(out, ipNet)
			continue
		}
		// Try parsing as single IP.
		ip := net.ParseIP(s)
		if ip == nil {
			continue
		}
		if ip4 := ip.To4(); ip4 != nil {
			ip = ip4
			out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(32, 32)})
		} else {
			out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)})
		}
	}
	return out
}

package httpx

import (
	"crypto/subtle"
	"strings"
	"sync/atomic"
)

// TokenSetLike is a token set used by AccessGuard.
//
// Implementations must be safe for concurrent use.
// The request path must be fast and must not block.
type TokenSetLike interface {
	Contains(token string) bool
}

// AtomicTokenSet is an updateable token set intended for hot changes.
//
// Read path (Contains) is lock-free and non-blocking.
// Write path (Update/AllowAll) is atomic and may allocate.
type AtomicTokenSet struct {
	snap atomic.Pointer[tokenSetSnapshot]
}

type tokenSetSnapshot struct {
	allowAll bool
	tokens   []string
}

// NewAtomicTokenSet creates a new token set in the deny-all state.
func NewAtomicTokenSet() *AtomicTokenSet {
	s := &AtomicTokenSet{}
	s.snap.Store(&tokenSetSnapshot{})
	return s
}

// Update replaces the current token snapshot.
//
// Semantics:
//   - tokens == nil: sets to empty (deny-all)
//   - blank/whitespace tokens are ignored
//   - if no valid tokens remain, it becomes empty (deny-all)
func (s *AtomicTokenSet) Update(tokens []string) {
	if s == nil {
		return
	}
	out := make([]string, 0, len(tokens))
	for _, raw := range tokens {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		out = append(out, t)
	}
	s.snap.Store(&tokenSetSnapshot{tokens: out})
}

// AllowAll sets this token set to allow all tokens.
func (s *AtomicTokenSet) AllowAll() {
	if s == nil {
		return
	}
	s.snap.Store(&tokenSetSnapshot{allowAll: true})
}

// Contains reports whether token is accepted.
//
// It is safe for concurrent use.
func (s *AtomicTokenSet) Contains(token string) bool {
	if s == nil {
		return false
	}
	snap := s.snap.Load()
	if snap == nil {
		return false
	}
	if snap.allowAll {
		return true
	}
	if len(snap.tokens) == 0 {
		return false
	}
	// Constant-time scan: do not early return on match.
	var ok bool
	for _, t := range snap.tokens {
		if constantTimeEqualString(token, t) {
			ok = true
		}
	}
	return ok
}

func (s *AtomicTokenSet) empty() bool {
	if s == nil {
		return true
	}
	snap := s.snap.Load()
	if snap == nil {
		return true
	}
	return !snap.allowAll && len(snap.tokens) == 0
}

type tokenSetValidator struct{ set TokenSetLike }

type tokenSetEmptyAware interface {
	TokenSetLike
	empty() bool
}

func (v tokenSetValidator) Validate(token string) (ok bool, reason DenyReason) {
	if v.set == nil {
		return false, DenyReasonTokenSetEmpty
	}
	if ea, ok := v.set.(tokenSetEmptyAware); ok && ea.empty() {
		return false, DenyReasonTokenSetEmpty
	}
	if v.set.Contains(token) {
		return true, ""
	}
	return false, DenyReasonTokenNotAllowed
}

type tokenCheckValidator struct{ fn func(token string) bool }

func (v tokenCheckValidator) Validate(token string) (ok bool, reason DenyReason) {
	if v.fn == nil {
		return false, DenyReasonTokenNotAllowed
	}
	if v.fn(token) {
		return true, ""
	}
	return false, DenyReasonTokenNotAllowed
}

func constantTimeEqualString(a, b string) bool {
	// Length check is not constant-time, but:
	//   - it avoids out-of-bounds reads
	//   - it avoids allocating/converting strings to []byte
	// Token length is not treated as sensitive here.
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return subtle.ConstantTimeByteEq(diff, 0) == 1
}

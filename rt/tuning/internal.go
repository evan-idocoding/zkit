package tuning

import (
	"bytes"
	"runtime"
)

type varEntry interface {
	key() string
	typ() Type

	snapshot() Item
	override() (OverrideItem, bool)

	setFromString(v string) error
}

func (t *Tuning) lockWrite() error {
	gid := curGoroutineID()
	if t.writeMu.TryLock() {
		t.writeOwner.Store(gid)
		return nil
	}
	// If we can't acquire immediately, attempt to detect re-entrancy.
	// This is a best-effort goroutine id; it is OK for this path to be a bit heavy,
	// because writes are expected to be rare and are not on a hot path.
	owner := t.writeOwner.Load()
	if gid != 0 && owner == gid {
		return ErrReentrantWrite
	}
	// Fail-safe: if goroutine id parsing fails and we can't prove it's not a re-entrant call,
	// returning an error is safer than risking a self-deadlock.
	if gid == 0 && owner == 0 {
		return ErrReentrantWrite
	}
	t.writeMu.Lock()
	t.writeOwner.Store(gid)
	return nil
}

func (t *Tuning) unlockWrite() {
	t.writeOwner.Store(0)
	t.writeMu.Unlock()
}

// curGoroutineID returns the current goroutine id by parsing runtime.Stack output.
//
// It returns 0 if parsing fails.
//
// Note: this is intentionally only used on the write path (Set/Reset*) to detect
// re-entrant writes from onChange callbacks.
func curGoroutineID() uint64 {
	// We intentionally keep this on the write path only.
	// Use a larger buffer to make parsing robust even if the runtime header format changes slightly.
	var buf [256]byte
	n := runtime.Stack(buf[:], false)
	b := buf[:n]
	// Format: "goroutine 123 [running]:\n"
	const prefix = "goroutine "
	if len(b) < len(prefix) || !bytes.HasPrefix(b, []byte(prefix)) {
		return 0
	}
	var id uint64
	i := len(prefix)
	for ; i < len(b); i++ {
		c := b[i]
		if c < '0' || c > '9' {
			break
		}
		id = id*10 + uint64(c-'0')
	}
	if id == 0 {
		return 0
	}
	return id
}

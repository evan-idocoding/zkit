package tuning

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Tuning holds a set of runtime-tunable variables.
//
// It is safe for concurrent use.
//
// The zero value is ready to use.
type Tuning struct {
	mu   sync.RWMutex
	vars map[string]varEntry

	// writeMu serializes writes (Set/Reset*) and onChange callbacks.
	// It is intentionally a global gate to keep semantics simple and stable.
	writeMu    sync.Mutex
	writeOwner atomic.Uint64 // goroutine id (best-effort), for re-entrant write detection.
}

// New creates a new Tuning registry.
func New() *Tuning {
	return &Tuning{vars: make(map[string]varEntry)}
}

var (
	defaultOnce sync.Once
	defaultT    *Tuning
)

// Default returns the process-wide default Tuning instance.
func Default() *Tuning {
	defaultOnce.Do(func() { defaultT = New() })
	return defaultT
}

// Snapshot returns a point-in-time view of all registered variables.
//
// Items are sorted by key (lexicographically) for stable output.
func (t *Tuning) Snapshot() Snapshot {
	if t == nil {
		return Snapshot{}
	}
	t.mu.RLock()
	items := make([]varEntry, 0, len(t.vars))
	for _, v := range t.vars {
		items = append(items, v)
	}
	t.mu.RUnlock()

	sort.Slice(items, func(i, j int) bool { return items[i].key() < items[j].key() })

	out := make([]Item, 0, len(items))
	for _, v := range items {
		out = append(out, v.snapshot())
	}
	return Snapshot{Items: out}
}

// ExportOverrides returns the current overrides (Value != DefaultValue).
//
// Items are sorted by key (lexicographically) for stable output.
func (t *Tuning) ExportOverrides() []OverrideItem {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	items := make([]varEntry, 0, len(t.vars))
	for _, v := range t.vars {
		items = append(items, v)
	}
	t.mu.RUnlock()

	sort.Slice(items, func(i, j int) bool { return items[i].key() < items[j].key() })

	out := make([]OverrideItem, 0, len(items))
	for _, v := range items {
		if ov, ok := v.override(); ok {
			out = append(out, ov)
		}
	}
	return out
}

// ExportOverridesJSON exports overrides as JSON bytes.
func (t *Tuning) ExportOverridesJSON() ([]byte, error) {
	if t == nil {
		return json.Marshal([]OverrideItem(nil))
	}
	return json.Marshal(t.ExportOverrides())
}

// SetFromString sets a registered key from its string representation (ops/admin usage).
//
// It does NOT change the type of a variable; the key must already be registered.
//
// Bool values are parsed in a slightly lenient way: case-insensitive true/false, t/f, 1/0,
// yes/no, y/n, on/off.
func (t *Tuning) SetFromString(key, value string) error {
	if t == nil {
		return fmt.Errorf("%w: nil Tuning", ErrInvalidConfig)
	}
	if err := validateKey(key); err != nil {
		return err
	}
	t.mu.RLock()
	v, ok := t.vars[key]
	t.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, key)
	}
	return v.setFromString(value)
}

// SetAny sets a registered key from a typed Go value (programmatic usage).
//
// This is useful when the caller only has a key string at runtime but still wants
// type safety. If the value type does not match the registered variable type,
// SetAny returns ErrTypeMismatch.
//
// Supported value types:
//   - bool
//   - int64
//   - float64
//   - string (for both StringVar and EnumVar)
//   - time.Duration
func (t *Tuning) SetAny(key string, value any) error {
	if t == nil {
		return fmt.Errorf("%w: nil Tuning", ErrInvalidConfig)
	}
	if err := validateKey(key); err != nil {
		return err
	}
	t.mu.RLock()
	v, ok := t.vars[key]
	t.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, key)
	}

	switch vv := v.(type) {
	case *BoolVar:
		x, ok := value.(bool)
		if !ok {
			return fmt.Errorf("%w: %q expects %s, got %T", ErrTypeMismatch, key, vv.typ(), value)
		}
		return vv.Set(x)
	case *Int64Var:
		x, ok := value.(int64)
		if !ok {
			return fmt.Errorf("%w: %q expects %s, got %T", ErrTypeMismatch, key, vv.typ(), value)
		}
		return vv.Set(x)
	case *Float64Var:
		x, ok := value.(float64)
		if !ok {
			return fmt.Errorf("%w: %q expects %s, got %T", ErrTypeMismatch, key, vv.typ(), value)
		}
		return vv.Set(x)
	case *StringVar:
		x, ok := value.(string)
		if !ok {
			return fmt.Errorf("%w: %q expects %s, got %T", ErrTypeMismatch, key, vv.typ(), value)
		}
		return vv.Set(x)
	case *DurationVar:
		x, ok := value.(time.Duration)
		if !ok {
			return fmt.Errorf("%w: %q expects %s, got %T", ErrTypeMismatch, key, vv.typ(), value)
		}
		return vv.Set(x)
	case *EnumVar:
		x, ok := value.(string)
		if !ok {
			return fmt.Errorf("%w: %q expects %s, got %T", ErrTypeMismatch, key, vv.typ(), value)
		}
		return vv.Set(x)
	default:
		// Should not happen.
		return fmt.Errorf("%w: %q unknown var type", ErrInvalidConfig, key)
	}
}

// Lookup returns a point-in-time view of a single registered key.
//
// It follows the same redaction rules as Snapshot(). If the key is invalid or not found,
// ok is false.
func (t *Tuning) Lookup(key string) (it Item, ok bool) {
	if t == nil {
		return Item{}, false
	}
	if err := validateKey(key); err != nil {
		return Item{}, false
	}
	t.mu.RLock()
	v, ok := t.vars[key]
	t.mu.RUnlock()
	if !ok {
		return Item{}, false
	}
	return v.snapshot(), true
}

// ResetToDefault resets a registered key back to its default value.
func (t *Tuning) ResetToDefault(key string) error {
	if t == nil {
		return fmt.Errorf("%w: nil Tuning", ErrInvalidConfig)
	}
	if err := validateKey(key); err != nil {
		return err
	}
	t.mu.RLock()
	v, ok := t.vars[key]
	t.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, key)
	}
	switch vv := v.(type) {
	case *BoolVar:
		return vv.ResetToDefault()
	case *Int64Var:
		return vv.ResetToDefault()
	case *Float64Var:
		return vv.ResetToDefault()
	case *StringVar:
		return vv.ResetToDefault()
	case *DurationVar:
		return vv.ResetToDefault()
	case *EnumVar:
		return vv.ResetToDefault()
	default:
		// Should not happen.
		return fmt.Errorf("%w: %q unknown var type", ErrInvalidConfig, key)
	}
}

// ResetToLastValue restores the previous effective value for a registered key (undo one step).
func (t *Tuning) ResetToLastValue(key string) error {
	if t == nil {
		return fmt.Errorf("%w: nil Tuning", ErrInvalidConfig)
	}
	if err := validateKey(key); err != nil {
		return err
	}
	t.mu.RLock()
	v, ok := t.vars[key]
	t.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, key)
	}
	switch vv := v.(type) {
	case *BoolVar:
		return vv.ResetToLastValue()
	case *Int64Var:
		return vv.ResetToLastValue()
	case *Float64Var:
		return vv.ResetToLastValue()
	case *StringVar:
		return vv.ResetToLastValue()
	case *DurationVar:
		return vv.ResetToLastValue()
	case *EnumVar:
		return vv.ResetToLastValue()
	default:
		// Should not happen.
		return fmt.Errorf("%w: %q unknown var type", ErrInvalidConfig, key)
	}
}

func (t *Tuning) register(key string, v varEntry) error {
	if t == nil {
		return fmt.Errorf("%w: nil Tuning", ErrInvalidConfig)
	}
	if err := validateKey(key); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.vars == nil {
		t.vars = make(map[string]varEntry)
	}
	if _, ok := t.vars[key]; ok {
		return fmt.Errorf("%w: %q", ErrAlreadyRegistered, key)
	}
	t.vars[key] = v
	return nil
}

func validateKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: empty", ErrInvalidKey)
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == '-':
		default:
			// Explicitly call out common footguns.
			if c == '/' {
				return fmt.Errorf("%w: %q contains '/' (not allowed)", ErrInvalidKey, key)
			}
			if strings.ContainsRune(" \t\r\n", rune(c)) {
				return fmt.Errorf("%w: %q contains whitespace (not allowed)", ErrInvalidKey, key)
			}
			return fmt.Errorf("%w: %q contains invalid char %q (allowed: [A-Za-z0-9._-])", ErrInvalidKey, key, c)
		}
	}
	return nil
}

package tuning

import (
	"fmt"
	"sync/atomic"
	"time"
)

type boolConfig struct {
	redact   bool
	onChange []func(bool)
}

// BoolOption configures a BoolVar at registration time.
type BoolOption func(*boolConfig)

// WithRedactBool enables redaction for Snapshot / ExportOverrides.
func WithRedactBool() BoolOption {
	return func(c *boolConfig) { c.redact = true }
}

// WithOnChangeBool appends an onChange callback.
//
// Callbacks are executed synchronously inside Set after the value is applied.
// Callbacks run even if the new value equals the current value.
// Callbacks must be fast and must not block. Panics are recovered and swallowed.
//
// If you care about panic visibility, wrap your callback and report it yourself:
//
//	WithOnChangeBool(func(v bool) {
//		defer func() {
//			if r := recover(); r != nil {
//				// report it (slog / fmt.Fprintf(os.Stderr, ...) / metrics / ...)
//			}
//		}()
//		// ... your logic ...
//	})
func WithOnChangeBool(fn func(newValue bool)) BoolOption {
	return func(c *boolConfig) {
		if fn != nil {
			c.onChange = append(c.onChange, fn)
		}
	}
}

// Bool registers a bool variable and returns its handle.
func (t *Tuning) Bool(key string, defaultValue bool, opts ...BoolOption) (*BoolVar, error) {
	if t == nil {
		return nil, fmt.Errorf("%w: nil Tuning", ErrInvalidConfig)
	}
	var cfg boolConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	v := &BoolVar{
		t:        t,
		k:        key,
		def:      defaultValue,
		redact:   cfg.redact,
		onChange: cfg.onChange,
	}
	v.cur.Store(defaultValue)
	v.source.Store(int32(SourceDefault))
	// lastUpdatedAt stays zero until the first runtime write.

	if err := t.register(key, v); err != nil {
		return nil, err
	}
	return v, nil
}

// BoolVar is a runtime-tunable bool parameter.
type BoolVar struct {
	t *Tuning
	k string

	def    bool
	redact bool

	cur    atomic.Bool
	source atomic.Int32 // Source

	lastUpdatedAtUnixNano atomic.Int64

	// last/hasLast are protected by Tuning's write gate.
	hasLast bool
	last    bool

	onChange []func(bool)
}

func (v *BoolVar) key() string { return v.k }
func (v *BoolVar) typ() Type   { return TypeBool }

// Key returns the variable key.
func (v *BoolVar) Key() string { return v.k }

// Get returns the current effective value.
//
// It is lock-free, allocation-free and non-blocking.
func (v *BoolVar) Get() bool { return v.cur.Load() }

// Source returns where the current effective value comes from.
func (v *BoolVar) Source() Source { return Source(v.source.Load()) }

// LastUpdatedAt returns the timestamp of the last successful runtime write (Set/Reset*).
// Zero means never updated.
func (v *BoolVar) LastUpdatedAt() time.Time {
	ns := v.lastUpdatedAtUnixNano.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// Set updates the value.
//
// It is thread-safe and blocking. It also triggers onChange callbacks synchronously.
func (v *BoolVar) Set(newValue bool) error {
	if v.t == nil {
		return fmt.Errorf("%w: nil tuning", ErrInvalidConfig)
	}
	if err := v.t.lockWrite(); err != nil {
		return err
	}
	defer v.t.unlockWrite()

	old := v.cur.Load()
	v.hasLast = true
	v.last = old

	v.cur.Store(newValue)
	if newValue == v.def {
		v.source.Store(int32(SourceDefault))
	} else {
		v.source.Store(int32(SourceRuntimeSet))
	}
	v.lastUpdatedAtUnixNano.Store(time.Now().UnixNano())

	for _, cb := range v.onChange {
		safeCallBool(cb, newValue)
	}
	return nil
}

// ResetToDefault sets the value back to the registered default value.
func (v *BoolVar) ResetToDefault() error { return v.Set(v.def) }

// ResetToLastValue restores the previous effective value (undo one step).
//
// After a successful ResetToLastValue, there is no further last value until the next successful Set.
func (v *BoolVar) ResetToLastValue() error {
	if v.t == nil {
		return fmt.Errorf("%w: nil tuning", ErrInvalidConfig)
	}
	if err := v.t.lockWrite(); err != nil {
		return err
	}
	defer v.t.unlockWrite()

	if !v.hasLast {
		return fmt.Errorf("%w: %q", ErrNoLastValue, v.k)
	}
	newValue := v.last
	v.hasLast = false

	v.cur.Store(newValue)
	if newValue == v.def {
		v.source.Store(int32(SourceDefault))
	} else {
		v.source.Store(int32(SourceRuntimeSet))
	}
	v.lastUpdatedAtUnixNano.Store(time.Now().UnixNano())

	for _, cb := range v.onChange {
		safeCallBool(cb, newValue)
	}
	return nil
}

func (v *BoolVar) setFromString(s string) error {
	b, ok := parseBoolLoose(s)
	if !ok {
		return fmt.Errorf("%w: %q expects bool (true/false, t/f, 1/0, yes/no, on/off), got %q", ErrInvalidValue, v.k, s)
	}
	return v.Set(b)
}

func (v *BoolVar) snapshot() Item {
	val := any(v.Get())
	def := any(v.def)
	if v.redact {
		val = "<redacted>"
		def = "<redacted>"
	}
	return Item{
		Key:           v.k,
		Type:          TypeBool,
		Value:         val,
		DefaultValue:  def,
		Source:        v.Source(),
		LastUpdatedAt: v.LastUpdatedAt(),
		Constraints:   Constraints{},
	}
}

func (v *BoolVar) override() (OverrideItem, bool) {
	cur := v.Get()
	if cur == v.def {
		return OverrideItem{}, false
	}
	if v.redact {
		return OverrideItem{Key: v.k, Type: TypeBool, Value: "<redacted>"}, true
	}
	if cur {
		return OverrideItem{Key: v.k, Type: TypeBool, Value: "true"}, true
	}
	return OverrideItem{Key: v.k, Type: TypeBool, Value: "false"}, true
}

func safeCallBool(fn func(bool), v bool) {
	if fn == nil {
		return
	}
	defer func() { _ = recover() }()
	fn(v)
}

package tuning

import (
	"fmt"
	"strconv"
	"sync/atomic"
	"time"
)

type int64Config struct {
	redact bool

	hasMin bool
	min    int64
	hasMax bool
	max    int64

	onChange []func(int64)
}

// Int64Option configures an Int64Var at registration time.
type Int64Option func(*int64Config)

// WithRedactInt64 enables redaction for Snapshot / ExportOverrides.
func WithRedactInt64() Int64Option {
	return func(c *int64Config) { c.redact = true }
}

// WithMinInt64 sets a minimum constraint (inclusive).
func WithMinInt64(min int64) Int64Option {
	return func(c *int64Config) {
		c.hasMin = true
		c.min = min
	}
}

// WithMaxInt64 sets a maximum constraint (inclusive).
func WithMaxInt64(max int64) Int64Option {
	return func(c *int64Config) {
		c.hasMax = true
		c.max = max
	}
}

// WithOnChangeInt64 appends an onChange callback.
//
// Callbacks are executed synchronously inside Set after the value is applied.
// Callbacks run even if the new value equals the current value.
// Callbacks must be fast and must not block. Panics are recovered and swallowed.
//
// If you care about panic visibility, wrap your callback and report it yourself.
func WithOnChangeInt64(fn func(newValue int64)) Int64Option {
	return func(c *int64Config) {
		if fn != nil {
			c.onChange = append(c.onChange, fn)
		}
	}
}

// Int64 registers an int64 variable and returns its handle.
func (t *Tuning) Int64(key string, defaultValue int64, opts ...Int64Option) (*Int64Var, error) {
	if t == nil {
		return nil, fmt.Errorf("%w: nil Tuning", ErrInvalidConfig)
	}
	var cfg int64Config
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.hasMin && cfg.hasMax && cfg.min > cfg.max {
		return nil, fmt.Errorf("%w: %q min(%d) > max(%d)", ErrInvalidConfig, key, cfg.min, cfg.max)
	}
	if err := validateInt64Value(key, defaultValue, cfg); err != nil {
		return nil, fmt.Errorf("%w: default value: %v", ErrInvalidConfig, err)
	}

	v := &Int64Var{
		t:        t,
		k:        key,
		def:      defaultValue,
		redact:   cfg.redact,
		hasMin:   cfg.hasMin,
		min:      cfg.min,
		hasMax:   cfg.hasMax,
		max:      cfg.max,
		onChange: cfg.onChange,
	}
	v.cur.Store(defaultValue)
	v.source.Store(int32(SourceDefault))

	if err := t.register(key, v); err != nil {
		return nil, err
	}
	return v, nil
}

// Int64Var is a runtime-tunable int64 parameter.
type Int64Var struct {
	t *Tuning
	k string

	def    int64
	redact bool

	hasMin bool
	min    int64
	hasMax bool
	max    int64

	cur    atomic.Int64
	source atomic.Int32 // Source

	lastUpdatedAtUnixNano atomic.Int64

	// last/hasLast are protected by Tuning's write gate.
	hasLast bool
	last    int64

	onChange []func(int64)
}

func (v *Int64Var) key() string { return v.k }
func (v *Int64Var) typ() Type   { return TypeInt64 }

func (v *Int64Var) Key() string { return v.k }

// Get returns the current effective value.
//
// It is lock-free, allocation-free and non-blocking.
func (v *Int64Var) Get() int64 { return v.cur.Load() }

func (v *Int64Var) Source() Source { return Source(v.source.Load()) }

func (v *Int64Var) LastUpdatedAt() time.Time {
	ns := v.lastUpdatedAtUnixNano.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

func (v *Int64Var) Set(newValue int64) error {
	if v.t == nil {
		return fmt.Errorf("%w: nil tuning", ErrInvalidConfig)
	}
	if err := validateInt64Value(v.k, newValue, int64Config{
		hasMin: v.hasMin, min: v.min,
		hasMax: v.hasMax, max: v.max,
	}); err != nil {
		return err
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
		safeCallInt64(cb, newValue)
	}
	return nil
}

func (v *Int64Var) ResetToDefault() error { return v.Set(v.def) }

func (v *Int64Var) ResetToLastValue() error {
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

	if err := validateInt64Value(v.k, newValue, int64Config{
		hasMin: v.hasMin, min: v.min,
		hasMax: v.hasMax, max: v.max,
	}); err != nil {
		return err
	}

	v.cur.Store(newValue)
	if newValue == v.def {
		v.source.Store(int32(SourceDefault))
	} else {
		v.source.Store(int32(SourceRuntimeSet))
	}
	v.lastUpdatedAtUnixNano.Store(time.Now().UnixNano())

	for _, cb := range v.onChange {
		safeCallInt64(cb, newValue)
	}
	return nil
}

func (v *Int64Var) setFromString(s string) error {
	n, err := parseInt64Base10(s)
	if err != nil {
		return fmt.Errorf("%w: %q expects int64 base10, got %q: %v", ErrInvalidValue, v.k, s, err)
	}
	return v.Set(n)
}

func (v *Int64Var) snapshot() Item {
	val := any(v.Get())
	def := any(v.def)
	if v.redact {
		val = "<redacted>"
		def = "<redacted>"
	}
	var minStr, maxStr *string
	if v.hasMin {
		s := strconv.FormatInt(v.min, 10)
		minStr = &s
	}
	if v.hasMax {
		s := strconv.FormatInt(v.max, 10)
		maxStr = &s
	}
	return Item{
		Key:           v.k,
		Type:          TypeInt64,
		Value:         val,
		DefaultValue:  def,
		Source:        v.Source(),
		LastUpdatedAt: v.LastUpdatedAt(),
		Constraints:   Constraints{Min: minStr, Max: maxStr},
	}
}

func (v *Int64Var) override() (OverrideItem, bool) {
	cur := v.Get()
	if cur == v.def {
		return OverrideItem{}, false
	}
	if v.redact {
		return OverrideItem{Key: v.k, Type: TypeInt64, Value: "<redacted>"}, true
	}
	return OverrideItem{Key: v.k, Type: TypeInt64, Value: strconv.FormatInt(cur, 10)}, true
}

func validateInt64Value(key string, v int64, cfg int64Config) error {
	if cfg.hasMin && v < cfg.min {
		return fmt.Errorf("%w: %q must be >= %d, got %d", ErrInvalidValue, key, cfg.min, v)
	}
	if cfg.hasMax && v > cfg.max {
		return fmt.Errorf("%w: %q must be <= %d, got %d", ErrInvalidValue, key, cfg.max, v)
	}
	return nil
}

func safeCallInt64(fn func(int64), v int64) {
	if fn == nil {
		return
	}
	defer func() { _ = recover() }()
	fn(v)
}

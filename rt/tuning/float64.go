package tuning

import (
	"fmt"
	"math"
	"strconv"
	"sync/atomic"
	"time"
)

type float64Config struct {
	redact bool

	hasMin bool
	min    float64
	hasMax bool
	max    float64

	onChange []func(float64)
}

// Float64Option configures a Float64Var at registration time.
type Float64Option func(*float64Config)

// WithRedactFloat64 enables redaction for Snapshot / ExportOverrides.
func WithRedactFloat64() Float64Option {
	return func(c *float64Config) { c.redact = true }
}

// WithMinFloat64 sets a minimum constraint (inclusive).
func WithMinFloat64(min float64) Float64Option {
	return func(c *float64Config) {
		c.hasMin = true
		c.min = min
	}
}

// WithMaxFloat64 sets a maximum constraint (inclusive).
func WithMaxFloat64(max float64) Float64Option {
	return func(c *float64Config) {
		c.hasMax = true
		c.max = max
	}
}

// WithOnChangeFloat64 appends an onChange callback.
//
// Callbacks are executed synchronously inside Set after the value is applied.
// Callbacks run even if the new value equals the current value.
// Callbacks must be fast and must not block. Panics are recovered and swallowed.
//
// If you care about panic visibility, wrap your callback and report it yourself.
func WithOnChangeFloat64(fn func(newValue float64)) Float64Option {
	return func(c *float64Config) {
		if fn != nil {
			c.onChange = append(c.onChange, fn)
		}
	}
}

// Float64 registers a float64 variable and returns its handle.
func (t *Tuning) Float64(key string, defaultValue float64, opts ...Float64Option) (*Float64Var, error) {
	if t == nil {
		return nil, fmt.Errorf("%w: nil Tuning", ErrInvalidConfig)
	}
	var cfg float64Config
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.hasMin && cfg.hasMax && cfg.min > cfg.max {
		return nil, fmt.Errorf("%w: %q min(%v) > max(%v)", ErrInvalidConfig, key, cfg.min, cfg.max)
	}
	if err := validateFloat64Value(key, defaultValue, cfg); err != nil {
		return nil, fmt.Errorf("%w: default value: %v", ErrInvalidConfig, err)
	}

	v := &Float64Var{
		t:          t,
		k:          key,
		defBits:    math.Float64bits(defaultValue),
		redact:     cfg.redact,
		hasMin:     cfg.hasMin,
		min:        cfg.min,
		hasMax:     cfg.hasMax,
		max:        cfg.max,
		onChange:   cfg.onChange,
		defForSnap: defaultValue,
	}
	v.curBits.Store(v.defBits)
	v.source.Store(int32(SourceDefault))

	if err := t.register(key, v); err != nil {
		return nil, err
	}
	return v, nil
}

// Float64Var is a runtime-tunable float64 parameter.
type Float64Var struct {
	t *Tuning
	k string

	redact bool

	hasMin bool
	min    float64
	hasMax bool
	max    float64

	curBits atomic.Uint64
	defBits uint64

	// defForSnap keeps the original default for Snapshot (to avoid bits->float surprises).
	defForSnap float64

	source atomic.Int32 // Source

	lastUpdatedAtUnixNano atomic.Int64

	// last/hasLast are protected by Tuning's write gate.
	hasLast  bool
	lastBits uint64

	onChange []func(float64)
}

func (v *Float64Var) key() string { return v.k }
func (v *Float64Var) typ() Type   { return TypeFloat64 }

func (v *Float64Var) Key() string { return v.k }

// Get returns the current effective value.
//
// It is lock-free, allocation-free and non-blocking.
func (v *Float64Var) Get() float64 {
	return math.Float64frombits(v.curBits.Load())
}

func (v *Float64Var) Source() Source { return Source(v.source.Load()) }

func (v *Float64Var) LastUpdatedAt() time.Time {
	ns := v.lastUpdatedAtUnixNano.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

func (v *Float64Var) Set(newValue float64) error {
	if v.t == nil {
		return fmt.Errorf("%w: nil tuning", ErrInvalidConfig)
	}
	if err := validateFloat64Value(v.k, newValue, float64Config{
		hasMin: v.hasMin, min: v.min,
		hasMax: v.hasMax, max: v.max,
	}); err != nil {
		return err
	}

	if err := v.t.lockWrite(); err != nil {
		return err
	}
	defer v.t.unlockWrite()

	old := v.curBits.Load()
	v.hasLast = true
	v.lastBits = old

	nb := math.Float64bits(newValue)
	v.curBits.Store(nb)
	if nb == v.defBits {
		v.source.Store(int32(SourceDefault))
	} else {
		v.source.Store(int32(SourceRuntimeSet))
	}
	v.lastUpdatedAtUnixNano.Store(time.Now().UnixNano())

	for _, cb := range v.onChange {
		safeCallFloat64(cb, newValue)
	}
	return nil
}

func (v *Float64Var) ResetToDefault() error { return v.Set(v.defForSnap) }

func (v *Float64Var) ResetToLastValue() error {
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
	nb := v.lastBits
	v.hasLast = false

	newValue := math.Float64frombits(nb)
	if err := validateFloat64Value(v.k, newValue, float64Config{
		hasMin: v.hasMin, min: v.min,
		hasMax: v.hasMax, max: v.max,
	}); err != nil {
		return err
	}

	v.curBits.Store(nb)
	if nb == v.defBits {
		v.source.Store(int32(SourceDefault))
	} else {
		v.source.Store(int32(SourceRuntimeSet))
	}
	v.lastUpdatedAtUnixNano.Store(time.Now().UnixNano())

	for _, cb := range v.onChange {
		safeCallFloat64(cb, newValue)
	}
	return nil
}

func (v *Float64Var) setFromString(s string) error {
	f, err := parseFloat64(s)
	if err != nil {
		return fmt.Errorf("%w: %q expects float64, got %q: %v", ErrInvalidValue, v.k, s, err)
	}
	return v.Set(f)
}

func (v *Float64Var) snapshot() Item {
	cur := v.Get()
	val := any(cur)
	def := any(v.defForSnap)
	if v.redact {
		val = "<redacted>"
		def = "<redacted>"
	}
	var minStr, maxStr *string
	if v.hasMin {
		s := strconv.FormatFloat(v.min, 'g', -1, 64)
		minStr = &s
	}
	if v.hasMax {
		s := strconv.FormatFloat(v.max, 'g', -1, 64)
		maxStr = &s
	}
	return Item{
		Key:           v.k,
		Type:          TypeFloat64,
		Value:         val,
		DefaultValue:  def,
		Source:        v.Source(),
		LastUpdatedAt: v.LastUpdatedAt(),
		Constraints:   Constraints{Min: minStr, Max: maxStr},
	}
}

func (v *Float64Var) override() (OverrideItem, bool) {
	cb := v.curBits.Load()
	if cb == v.defBits {
		return OverrideItem{}, false
	}
	if v.redact {
		return OverrideItem{Key: v.k, Type: TypeFloat64, Value: "<redacted>"}, true
	}
	cur := math.Float64frombits(cb)
	return OverrideItem{Key: v.k, Type: TypeFloat64, Value: strconv.FormatFloat(cur, 'g', -1, 64)}, true
}

func validateFloat64Value(key string, v float64, cfg float64Config) error {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fmt.Errorf("%w: %q must be finite, got %v", ErrInvalidValue, key, v)
	}
	if cfg.hasMin && v < cfg.min {
		return fmt.Errorf("%w: %q must be >= %v, got %v", ErrInvalidValue, key, cfg.min, v)
	}
	if cfg.hasMax && v > cfg.max {
		return fmt.Errorf("%w: %q must be <= %v, got %v", ErrInvalidValue, key, cfg.max, v)
	}
	return nil
}

func safeCallFloat64(fn func(float64), v float64) {
	if fn == nil {
		return
	}
	defer func() { _ = recover() }()
	fn(v)
}

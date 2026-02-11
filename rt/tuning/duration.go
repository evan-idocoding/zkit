package tuning

import (
	"fmt"
	"sync/atomic"
	"time"
)

type durationConfig struct {
	redact bool

	hasMin bool
	min    time.Duration
	hasMax bool
	max    time.Duration

	onChange []func(time.Duration)
}

// DurationOption configures a DurationVar at registration time.
type DurationOption func(*durationConfig)

// WithRedactDuration enables redaction for Snapshot / ExportOverrides.
func WithRedactDuration() DurationOption {
	return func(c *durationConfig) { c.redact = true }
}

// WithMinDuration sets a minimum constraint (inclusive).
func WithMinDuration(min time.Duration) DurationOption {
	return func(c *durationConfig) {
		c.hasMin = true
		c.min = min
	}
}

// WithMaxDuration sets a maximum constraint (inclusive).
func WithMaxDuration(max time.Duration) DurationOption {
	return func(c *durationConfig) {
		c.hasMax = true
		c.max = max
	}
}

// WithOnChangeDuration appends an onChange callback.
//
// Callbacks are executed synchronously inside Set after the value is applied.
// Callbacks run even if the new value equals the current value.
// Callbacks must be fast and must not block. Panics are recovered and swallowed.
//
// If you care about panic visibility, wrap your callback and report it yourself.
func WithOnChangeDuration(fn func(newValue time.Duration)) DurationOption {
	return func(c *durationConfig) {
		if fn != nil {
			c.onChange = append(c.onChange, fn)
		}
	}
}

// Duration registers a time.Duration variable and returns its handle.
func (t *Tuning) Duration(key string, defaultValue time.Duration, opts ...DurationOption) (*DurationVar, error) {
	if t == nil {
		return nil, fmt.Errorf("%w: nil Tuning", ErrInvalidConfig)
	}
	var cfg durationConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.hasMin && cfg.hasMax && cfg.min > cfg.max {
		return nil, fmt.Errorf("%w: %q min(%s) > max(%s)", ErrInvalidConfig, key, cfg.min, cfg.max)
	}
	if err := validateDurationValue(key, defaultValue, cfg); err != nil {
		return nil, fmt.Errorf("%w: default value: %v", ErrInvalidConfig, err)
	}

	v := &DurationVar{
		t:        t,
		k:        key,
		def:      defaultValue,
		defNanos: defaultValue.Nanoseconds(),
		redact:   cfg.redact,
		hasMin:   cfg.hasMin,
		min:      cfg.min,
		hasMax:   cfg.hasMax,
		max:      cfg.max,
		onChange: cfg.onChange,
	}
	v.curNanos.Store(v.defNanos)
	v.source.Store(int32(SourceDefault))

	if err := t.register(key, v); err != nil {
		return nil, err
	}
	return v, nil
}

// DurationVar is a runtime-tunable time.Duration parameter.
type DurationVar struct {
	t *Tuning
	k string

	def      time.Duration
	defNanos int64
	redact   bool

	hasMin bool
	min    time.Duration
	hasMax bool
	max    time.Duration

	curNanos atomic.Int64
	source   atomic.Int32 // Source

	lastUpdatedAtUnixNano atomic.Int64

	// last/hasLast are protected by Tuning's write gate.
	hasLast bool
	last    time.Duration

	onChange []func(time.Duration)
}

func (v *DurationVar) key() string { return v.k }
func (v *DurationVar) typ() Type   { return TypeDuration }

func (v *DurationVar) Key() string { return v.k }

// Get returns the current effective value.
//
// It is lock-free, allocation-free and non-blocking.
func (v *DurationVar) Get() time.Duration {
	return time.Duration(v.curNanos.Load())
}

func (v *DurationVar) Source() Source { return Source(v.source.Load()) }

func (v *DurationVar) LastUpdatedAt() time.Time {
	ns := v.lastUpdatedAtUnixNano.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

func (v *DurationVar) Set(newValue time.Duration) error {
	if v.t == nil {
		return fmt.Errorf("%w: nil tuning", ErrInvalidConfig)
	}
	if err := validateDurationValue(v.k, newValue, durationConfig{
		hasMin: v.hasMin, min: v.min,
		hasMax: v.hasMax, max: v.max,
	}); err != nil {
		return err
	}
	if err := v.t.lockWrite(); err != nil {
		return err
	}
	defer v.t.unlockWrite()

	old := v.Get()
	v.hasLast = true
	v.last = old

	nanos := newValue.Nanoseconds()
	v.curNanos.Store(nanos)
	if nanos == v.defNanos {
		v.source.Store(int32(SourceDefault))
	} else {
		v.source.Store(int32(SourceRuntimeSet))
	}
	v.lastUpdatedAtUnixNano.Store(time.Now().UnixNano())

	for _, cb := range v.onChange {
		safeCallDuration(cb, newValue)
	}
	return nil
}

func (v *DurationVar) ResetToDefault() error { return v.Set(v.def) }

func (v *DurationVar) ResetToLastValue() error {
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

	if err := validateDurationValue(v.k, newValue, durationConfig{
		hasMin: v.hasMin, min: v.min,
		hasMax: v.hasMax, max: v.max,
	}); err != nil {
		return err
	}

	nanos := newValue.Nanoseconds()
	v.curNanos.Store(nanos)
	if nanos == v.defNanos {
		v.source.Store(int32(SourceDefault))
	} else {
		v.source.Store(int32(SourceRuntimeSet))
	}
	v.lastUpdatedAtUnixNano.Store(time.Now().UnixNano())

	for _, cb := range v.onChange {
		safeCallDuration(cb, newValue)
	}
	return nil
}

func (v *DurationVar) setFromString(s string) error {
	d, err := parseDuration(s)
	if err != nil {
		return fmt.Errorf("%w: %q expects duration (Go format), got %q: %v", ErrInvalidValue, v.k, s, err)
	}
	return v.Set(d)
}

func (v *DurationVar) snapshot() Item {
	cur := v.Get()
	val := any(cur)
	def := any(v.def)
	if v.redact {
		val = "<redacted>"
		def = "<redacted>"
	}
	var minStr, maxStr *string
	if v.hasMin {
		s := v.min.String()
		minStr = &s
	}
	if v.hasMax {
		s := v.max.String()
		maxStr = &s
	}
	return Item{
		Key:           v.k,
		Type:          TypeDuration,
		Value:         val,
		DefaultValue:  def,
		Source:        v.Source(),
		LastUpdatedAt: v.LastUpdatedAt(),
		Constraints:   Constraints{Min: minStr, Max: maxStr},
	}
}

func (v *DurationVar) override() (OverrideItem, bool) {
	curNanos := v.curNanos.Load()
	if curNanos == v.defNanos {
		return OverrideItem{}, false
	}
	if v.redact {
		return OverrideItem{Key: v.k, Type: TypeDuration, Value: "<redacted>"}, true
	}
	return OverrideItem{Key: v.k, Type: TypeDuration, Value: time.Duration(curNanos).String()}, true
}

func validateDurationValue(key string, v time.Duration, cfg durationConfig) error {
	if cfg.hasMin && v < cfg.min {
		return fmt.Errorf("%w: %q must be >= %s, got %s", ErrInvalidValue, key, cfg.min, v)
	}
	if cfg.hasMax && v > cfg.max {
		return fmt.Errorf("%w: %q must be <= %s, got %s", ErrInvalidValue, key, cfg.max, v)
	}
	return nil
}

func safeCallDuration(fn func(time.Duration), v time.Duration) {
	if fn == nil {
		return
	}
	defer func() { _ = recover() }()
	fn(v)
}

package tuning

import (
	"fmt"
	"sync/atomic"
	"time"
)

type stringConfig struct {
	redact   bool
	nonEmpty bool
	onChange []func(string)
}

// StringOption configures a StringVar at registration time.
type StringOption func(*stringConfig)

// WithRedactString enables redaction for Snapshot / ExportOverrides.
func WithRedactString() StringOption {
	return func(c *stringConfig) { c.redact = true }
}

// WithNonEmptyString enforces a non-empty string constraint.
func WithNonEmptyString() StringOption {
	return func(c *stringConfig) { c.nonEmpty = true }
}

// WithOnChangeString appends an onChange callback.
//
// Callbacks are executed synchronously inside Set after the value is applied.
// Callbacks run even if the new value equals the current value.
// Callbacks must be fast and must not block. Panics are recovered and swallowed.
//
// If you care about panic visibility, wrap your callback and report it yourself.
func WithOnChangeString(fn func(newValue string)) StringOption {
	return func(c *stringConfig) {
		if fn != nil {
			c.onChange = append(c.onChange, fn)
		}
	}
}

// String registers a string variable and returns its handle.
func (t *Tuning) String(key string, defaultValue string, opts ...StringOption) (*StringVar, error) {
	if t == nil {
		return nil, fmt.Errorf("%w: nil Tuning", ErrInvalidConfig)
	}
	var cfg stringConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.nonEmpty && defaultValue == "" {
		return nil, fmt.Errorf("%w: %q default value must be non-empty", ErrInvalidConfig, key)
	}

	v := &StringVar{
		t:        t,
		k:        key,
		def:      defaultValue,
		redact:   cfg.redact,
		nonEmpty: cfg.nonEmpty,
		onChange: cfg.onChange,
	}
	v.curPtr.Store(ptrToString(defaultValue))
	v.source.Store(int32(SourceDefault))

	if err := t.register(key, v); err != nil {
		return nil, err
	}
	return v, nil
}

// StringVar is a runtime-tunable string parameter.
type StringVar struct {
	t *Tuning
	k string

	def    string
	redact bool

	nonEmpty bool

	curPtr atomic.Pointer[string]
	source atomic.Int32 // Source

	lastUpdatedAtUnixNano atomic.Int64

	// last/hasLast are protected by Tuning's write gate.
	hasLast bool
	last    string

	onChange []func(string)
}

func (v *StringVar) key() string { return v.k }
func (v *StringVar) typ() Type   { return TypeString }

func (v *StringVar) Key() string { return v.k }

// Get returns the current effective value.
//
// It is lock-free, allocation-free and non-blocking.
func (v *StringVar) Get() string {
	p := v.curPtr.Load()
	if p == nil {
		return ""
	}
	return *p
}

func (v *StringVar) Source() Source { return Source(v.source.Load()) }

func (v *StringVar) LastUpdatedAt() time.Time {
	ns := v.lastUpdatedAtUnixNano.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

func (v *StringVar) Set(newValue string) error {
	if v.t == nil {
		return fmt.Errorf("%w: nil tuning", ErrInvalidConfig)
	}
	if v.nonEmpty && newValue == "" {
		return fmt.Errorf("%w: %q must be non-empty", ErrInvalidValue, v.k)
	}
	if err := v.t.lockWrite(); err != nil {
		return err
	}
	defer v.t.unlockWrite()

	old := v.Get()
	v.hasLast = true
	v.last = old

	v.curPtr.Store(ptrToString(newValue))
	if newValue == v.def {
		v.source.Store(int32(SourceDefault))
	} else {
		v.source.Store(int32(SourceRuntimeSet))
	}
	v.lastUpdatedAtUnixNano.Store(time.Now().UnixNano())

	for _, cb := range v.onChange {
		safeCallString(cb, newValue)
	}
	return nil
}

func (v *StringVar) ResetToDefault() error { return v.Set(v.def) }

func (v *StringVar) ResetToLastValue() error {
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

	if v.nonEmpty && newValue == "" {
		return fmt.Errorf("%w: %q must be non-empty", ErrInvalidValue, v.k)
	}

	v.curPtr.Store(ptrToString(newValue))
	if newValue == v.def {
		v.source.Store(int32(SourceDefault))
	} else {
		v.source.Store(int32(SourceRuntimeSet))
	}
	v.lastUpdatedAtUnixNano.Store(time.Now().UnixNano())

	for _, cb := range v.onChange {
		safeCallString(cb, newValue)
	}
	return nil
}

func (v *StringVar) setFromString(s string) error {
	// String values are taken as-is (no trimming).
	return v.Set(s)
}

func (v *StringVar) snapshot() Item {
	cur := v.Get()
	val := any(cur)
	def := any(v.def)
	if v.redact {
		val = "<redacted>"
		def = "<redacted>"
	}
	return Item{
		Key:           v.k,
		Type:          TypeString,
		Value:         val,
		DefaultValue:  def,
		Source:        v.Source(),
		LastUpdatedAt: v.LastUpdatedAt(),
		Constraints:   Constraints{NonEmpty: v.nonEmpty},
	}
}

func (v *StringVar) override() (OverrideItem, bool) {
	cur := v.Get()
	if cur == v.def {
		return OverrideItem{}, false
	}
	if v.redact {
		return OverrideItem{Key: v.k, Type: TypeString, Value: "<redacted>"}, true
	}
	return OverrideItem{Key: v.k, Type: TypeString, Value: cur}, true
}

func ptrToString(s string) *string {
	p := new(string)
	*p = s
	return p
}

func safeCallString(fn func(string), v string) {
	if fn == nil {
		return
	}
	defer func() { _ = recover() }()
	fn(v)
}

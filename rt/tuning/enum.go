package tuning

import (
	"fmt"
	"sync/atomic"
	"time"
)

type enumConfig struct {
	redact bool

	allowed []string
	// normalize can be used to implement case-insensitive or alias-friendly enums.
	// If set, it is applied to default and all Set/SetFromString inputs before validation.
	normalize func(string) (string, bool)

	onChange []func(string)
}

// EnumOption configures an EnumVar at registration time.
type EnumOption func(*enumConfig)

// WithRedactEnum enables redaction for Snapshot / ExportOverrides.
func WithRedactEnum() EnumOption {
	return func(c *enumConfig) { c.redact = true }
}

// WithEnumAllowed sets the allowed values for an enum.
//
// allowed must be non-empty and must not contain duplicates.
// The order is preserved and used in Snapshot / Export output.
func WithEnumAllowed(allowed ...string) EnumOption {
	return func(c *enumConfig) {
		if len(allowed) == 0 {
			return
		}
		// Copy to avoid surprising mutations by callers.
		c.allowed = append([]string(nil), allowed...)
	}
}

// WithEnumNormalize sets an optional normalizer applied to default and all inputs.
//
// This is primarily intended for small helpers (e.g. tuningslog) that want
// case-insensitive inputs while still storing a canonical value.
//
// If normalize returns ok=false, the value is rejected as invalid.
func WithEnumNormalize(normalize func(string) (string, bool)) EnumOption {
	return func(c *enumConfig) { c.normalize = normalize }
}

// WithOnChangeEnum appends an onChange callback.
//
// Callbacks are executed synchronously inside Set after the value is applied.
// Callbacks run even if the new value equals the current value.
// Callbacks must be fast and must not block. Panics are recovered and swallowed.
//
// If you care about panic visibility, wrap your callback and report it yourself.
func WithOnChangeEnum(fn func(newValue string)) EnumOption {
	return func(c *enumConfig) {
		if fn != nil {
			c.onChange = append(c.onChange, fn)
		}
	}
}

// Enum registers a string enum variable and returns its handle.
func (t *Tuning) Enum(key string, defaultValue string, opts ...EnumOption) (*EnumVar, error) {
	if t == nil {
		return nil, fmt.Errorf("%w: nil Tuning", ErrInvalidConfig)
	}
	var cfg enumConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if len(cfg.allowed) == 0 {
		return nil, fmt.Errorf("%w: %q enum allowed list is required", ErrInvalidConfig, key)
	}

	if cfg.normalize != nil {
		nv, ok := cfg.normalize(defaultValue)
		if !ok {
			return nil, fmt.Errorf("%w: %q default enum value rejected by normalizer: %q", ErrInvalidConfig, key, defaultValue)
		}
		defaultValue = nv
	}

	index := make(map[string]uint32, len(cfg.allowed))
	for i, s := range cfg.allowed {
		if _, ok := index[s]; ok {
			return nil, fmt.Errorf("%w: %q enum allowed contains duplicate %q", ErrInvalidConfig, key, s)
		}
		index[s] = uint32(i)
	}
	defIdx, ok := index[defaultValue]
	if !ok {
		return nil, fmt.Errorf("%w: %q default enum value %q not in allowed list", ErrInvalidConfig, key, defaultValue)
	}

	v := &EnumVar{
		t:         t,
		k:         key,
		allowed:   append([]string(nil), cfg.allowed...),
		index:     index,
		defIdx:    defIdx,
		defValue:  defaultValue,
		redact:    cfg.redact,
		normalize: cfg.normalize,
		onChange:  cfg.onChange,
	}
	v.curIdx.Store(defIdx)
	v.source.Store(int32(SourceDefault))

	if err := t.register(key, v); err != nil {
		return nil, err
	}
	return v, nil
}

// EnumVar is a runtime-tunable string enum parameter.
type EnumVar struct {
	t *Tuning
	k string

	allowed []string
	index   map[string]uint32

	defIdx   uint32
	defValue string

	redact bool

	normalize func(string) (string, bool)

	curIdx atomic.Uint32
	source atomic.Int32 // Source

	lastUpdatedAtUnixNano atomic.Int64

	// last/hasLast are protected by Tuning's write gate.
	hasLast bool
	lastIdx uint32

	onChange []func(string)
}

func (v *EnumVar) key() string { return v.k }
func (v *EnumVar) typ() Type   { return TypeEnum }

func (v *EnumVar) Key() string { return v.k }

// Allowed returns a copy of the allowed values in stable order.
func (v *EnumVar) Allowed() []string { return append([]string(nil), v.allowed...) }

// Get returns the current effective value.
//
// It is lock-free, allocation-free and non-blocking.
func (v *EnumVar) Get() string {
	i := v.curIdx.Load()
	if int(i) >= len(v.allowed) {
		return ""
	}
	return v.allowed[i]
}

func (v *EnumVar) Source() Source { return Source(v.source.Load()) }

func (v *EnumVar) LastUpdatedAt() time.Time {
	ns := v.lastUpdatedAtUnixNano.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

func (v *EnumVar) Set(newValue string) error {
	if v.t == nil {
		return fmt.Errorf("%w: nil tuning", ErrInvalidConfig)
	}
	idx, err := v.parseValue(newValue)
	if err != nil {
		return err
	}

	if err := v.t.lockWrite(); err != nil {
		return err
	}
	defer v.t.unlockWrite()

	old := v.curIdx.Load()
	v.hasLast = true
	v.lastIdx = old

	v.curIdx.Store(idx)
	if idx == v.defIdx {
		v.source.Store(int32(SourceDefault))
	} else {
		v.source.Store(int32(SourceRuntimeSet))
	}
	v.lastUpdatedAtUnixNano.Store(time.Now().UnixNano())

	val := v.allowed[idx]
	for _, cb := range v.onChange {
		safeCallEnum(cb, val)
	}
	return nil
}

func (v *EnumVar) ResetToDefault() error { return v.Set(v.defValue) }

func (v *EnumVar) ResetToLastValue() error {
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
	idx := v.lastIdx
	v.hasLast = false

	if int(idx) >= len(v.allowed) {
		return fmt.Errorf("%w: %q last enum index out of range", ErrInvalidConfig, v.k)
	}

	v.curIdx.Store(idx)
	if idx == v.defIdx {
		v.source.Store(int32(SourceDefault))
	} else {
		v.source.Store(int32(SourceRuntimeSet))
	}
	v.lastUpdatedAtUnixNano.Store(time.Now().UnixNano())

	val := v.allowed[idx]
	for _, cb := range v.onChange {
		safeCallEnum(cb, val)
	}
	return nil
}

func (v *EnumVar) setFromString(s string) error {
	return v.Set(s)
}

func (v *EnumVar) snapshot() Item {
	cur := v.Get()
	val := any(cur)
	def := any(v.defValue)
	if v.redact {
		val = "<redacted>"
		def = "<redacted>"
	}
	return Item{
		Key:           v.k,
		Type:          TypeEnum,
		Value:         val,
		DefaultValue:  def,
		Source:        v.Source(),
		LastUpdatedAt: v.LastUpdatedAt(),
		Constraints:   Constraints{EnumAllowed: append([]string(nil), v.allowed...)},
	}
}

func (v *EnumVar) override() (OverrideItem, bool) {
	idx := v.curIdx.Load()
	if idx == v.defIdx {
		return OverrideItem{}, false
	}
	if v.redact {
		return OverrideItem{Key: v.k, Type: TypeEnum, Value: "<redacted>"}, true
	}
	if int(idx) >= len(v.allowed) {
		return OverrideItem{Key: v.k, Type: TypeEnum, Value: ""}, true
	}
	return OverrideItem{Key: v.k, Type: TypeEnum, Value: v.allowed[idx]}, true
}

func (v *EnumVar) parseValue(s string) (uint32, error) {
	if v.normalize != nil {
		ns, ok := v.normalize(s)
		if !ok {
			return 0, fmt.Errorf("%w: %q enum value rejected by normalizer: %q", ErrInvalidValue, v.k, s)
		}
		s = ns
	}
	idx, ok := v.index[s]
	if !ok {
		return 0, fmt.Errorf("%w: %q enum value %q not in allowed list", ErrInvalidValue, v.k, s)
	}
	return idx, nil
}

func safeCallEnum(fn func(string), v string) {
	if fn == nil {
		return
	}
	defer func() { _ = recover() }()
	fn(v)
}

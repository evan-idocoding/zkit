package tuning

import (
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"
)

func TestKeyValidation(t *testing.T) {
	tu := New()

	if _, err := tu.Bool("", false); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey, got %v", err)
	}
	if _, err := tu.Bool("a/b", false); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey, got %v", err)
	}
	if _, err := tu.Bool("a b", false); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey, got %v", err)
	}
	if _, err := tu.Bool("a$", false); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey, got %v", err)
	}
}

func TestAlreadyRegistered(t *testing.T) {
	tu := New()
	if _, err := tu.Bool("b", false); err != nil {
		t.Fatal(err)
	}
	if _, err := tu.Bool("b", false); !errors.Is(err, ErrAlreadyRegistered) {
		t.Fatalf("expected ErrAlreadyRegistered, got %v", err)
	}
}

func TestZeroValueTuningUsable(t *testing.T) {
	var tu Tuning // zero value
	v, err := tu.Bool("b", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Get(); got != false {
		t.Fatalf("expected false, got %v", got)
	}
	if err := tu.SetFromString("b", "true"); err != nil {
		t.Fatal(err)
	}
	if got := v.Get(); got != true {
		t.Fatalf("expected true, got %v", got)
	}
}

func TestSetFromStringNotFound(t *testing.T) {
	tu := New()
	if err := tu.SetFromString("missing", "1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetNoAllocs(t *testing.T) {
	tu := New()

	b, err := tu.Bool("b", true)
	if err != nil {
		t.Fatal(err)
	}
	i, err := tu.Int64("i", 1)
	if err != nil {
		t.Fatal(err)
	}
	f, err := tu.Float64("f", 0.25)
	if err != nil {
		t.Fatal(err)
	}
	s, err := tu.String("s", "x")
	if err != nil {
		t.Fatal(err)
	}
	d, err := tu.Duration("d", 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	e, err := tu.Enum("e", "a", WithEnumAllowed("a", "b"))
	if err != nil {
		t.Fatal(err)
	}

	if n := testing.AllocsPerRun(1000, func() { _ = b.Get() }); n != 0 {
		t.Fatalf("BoolVar.Get allocs=%v", n)
	}
	if n := testing.AllocsPerRun(1000, func() { _ = i.Get() }); n != 0 {
		t.Fatalf("Int64Var.Get allocs=%v", n)
	}
	if n := testing.AllocsPerRun(1000, func() { _ = f.Get() }); n != 0 {
		t.Fatalf("Float64Var.Get allocs=%v", n)
	}
	if n := testing.AllocsPerRun(1000, func() { _ = s.Get() }); n != 0 {
		t.Fatalf("StringVar.Get allocs=%v", n)
	}
	if n := testing.AllocsPerRun(1000, func() { _ = d.Get() }); n != 0 {
		t.Fatalf("DurationVar.Get allocs=%v", n)
	}
	if n := testing.AllocsPerRun(1000, func() { _ = e.Get() }); n != 0 {
		t.Fatalf("EnumVar.Get allocs=%v", n)
	}
}

func TestOnChangeCalledEvenIfSame(t *testing.T) {
	tu := New()
	var n atomic.Int64
	b, err := tu.Bool("b", false, WithOnChangeBool(func(bool) { n.Add(1) }))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Set(false); err != nil {
		t.Fatal(err)
	}
	if err := b.Set(false); err != nil {
		t.Fatal(err)
	}
	if got := n.Load(); got != 2 {
		t.Fatalf("expected 2 callbacks, got %d", got)
	}
}

func TestOnChangeOrderAndPanicRecovered(t *testing.T) {
	tu := New()
	var seq atomic.Int64
	b, err := tu.Bool("b", false,
		WithOnChangeBool(func(bool) { seq.Store(seq.Load()*10 + 1) }),
		WithOnChangeBool(func(bool) { panic("boom") }),
		WithOnChangeBool(func(bool) { seq.Store(seq.Load()*10 + 3) }),
	)
	if err != nil {
		t.Fatal(err)
	}
	// Set should still succeed even if a callback panics.
	if err := b.Set(true); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if got := seq.Load(); got != 13 {
		t.Fatalf("expected callback order 1 then 3 (panic swallowed), got %d", got)
	}
}

func TestResetToLastValueOneStep(t *testing.T) {
	tu := New()
	v, err := tu.Int64("x", 1)
	if err != nil {
		t.Fatal(err)
	}
	_ = v.Set(2)
	_ = v.Set(3)

	if err := v.ResetToLastValue(); err != nil {
		t.Fatal(err)
	}
	if got := v.Get(); got != 2 {
		t.Fatalf("expected 2, got %d", got)
	}
	if err := v.ResetToLastValue(); !errors.Is(err, ErrNoLastValue) {
		t.Fatalf("expected ErrNoLastValue, got %v", err)
	}
}

func TestLookupNotFoundAndInvalidKey(t *testing.T) {
	tu := New()
	_, _ = tu.Bool("b", false)

	if _, ok := tu.Lookup("missing"); ok {
		t.Fatalf("expected ok=false for missing key")
	}
	if _, ok := tu.Lookup("a/b"); ok {
		t.Fatalf("expected ok=false for invalid key")
	}
}

func TestLookupRedaction(t *testing.T) {
	tu := New()
	secret, err := tu.String("secret", "dflt", WithRedactString())
	if err != nil {
		t.Fatal(err)
	}
	_ = secret.Set("runtime")

	it, ok := tu.Lookup("secret")
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if it.Key != "secret" {
		t.Fatalf("expected key secret, got %q", it.Key)
	}
	if it.Value != "<redacted>" || it.DefaultValue != "<redacted>" {
		t.Fatalf("expected redacted value+default, got value=%v default=%v", it.Value, it.DefaultValue)
	}
}

func TestResetToDefaultByKey(t *testing.T) {
	tu := New()
	b, err := tu.Bool("b", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Set(true); err != nil {
		t.Fatal(err)
	}
	if got := b.Source(); got != SourceRuntimeSet {
		t.Fatalf("expected SourceRuntimeSet after Set(true), got %v", got)
	}
	if err := tu.ResetToDefault("b"); err != nil {
		t.Fatal(err)
	}
	if got := b.Get(); got != false {
		t.Fatalf("expected false after reset, got %v", got)
	}
	if got := b.Source(); got != SourceDefault {
		t.Fatalf("expected SourceDefault after reset, got %v", got)
	}
	if b.LastUpdatedAt().IsZero() {
		t.Fatalf("expected non-zero LastUpdatedAt after reset")
	}
}

func TestResetToLastValueByKeyOneStep(t *testing.T) {
	tu := New()
	v, err := tu.Int64("x", 1)
	if err != nil {
		t.Fatal(err)
	}
	_ = v.Set(2)
	_ = v.Set(3)

	if err := tu.ResetToLastValue("x"); err != nil {
		t.Fatal(err)
	}
	if got := v.Get(); got != 2 {
		t.Fatalf("expected 2, got %d", got)
	}
	if err := tu.ResetToLastValue("x"); !errors.Is(err, ErrNoLastValue) {
		t.Fatalf("expected ErrNoLastValue, got %v", err)
	}
}

func TestResetByKeyNotFound(t *testing.T) {
	tu := New()
	if err := tu.ResetToDefault("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := tu.ResetToLastValue("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestReentrantWriteDetectedViaResetByKey(t *testing.T) {
	tu := New()

	var innerErr atomic.Value
	b, err := tu.Bool("b", false, WithOnChangeBool(func(bool) {
		innerErr.Store(tu.ResetToDefault("b"))
	}))
	if err != nil {
		t.Fatal(err)
	}

	if err := b.Set(true); err != nil {
		t.Fatalf("outer Set should succeed, got %v", err)
	}
	if got, _ := innerErr.Load().(error); !errors.Is(got, ErrReentrantWrite) {
		t.Fatalf("expected ErrReentrantWrite, got %v", got)
	}
}

func TestStringNonEmptyConstraint(t *testing.T) {
	tu := New()
	if _, err := tu.String("s", "", WithNonEmptyString()); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
	s, err := tu.String("s2", "x", WithNonEmptyString())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set(""); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("expected ErrInvalidValue, got %v", err)
	}
}

func TestInt64MinMaxConstraint(t *testing.T) {
	tu := New()
	if _, err := tu.Int64("bad", 1, WithMinInt64(10), WithMaxInt64(0)); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}

	v, err := tu.Int64("x", 10, WithMinInt64(0), WithMaxInt64(10))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Set(11); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("expected ErrInvalidValue, got %v", err)
	}
}

func TestDurationMinMaxConstraint(t *testing.T) {
	tu := New()
	v, err := tu.Duration("d", 10*time.Millisecond, WithMinDuration(0), WithMaxDuration(10*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Set(11 * time.Millisecond); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("expected ErrInvalidValue, got %v", err)
	}
}

func TestFloat64FiniteAndMinMaxConstraint(t *testing.T) {
	tu := New()
	v, err := tu.Float64("f", 0.5, WithMinFloat64(0.0), WithMaxFloat64(1.0))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Set(math.NaN()); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("expected ErrInvalidValue for NaN, got %v", err)
	}
	if err := v.Set(math.Inf(1)); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("expected ErrInvalidValue for Inf, got %v", err)
	}
	if err := v.Set(2.0); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("expected ErrInvalidValue for > max, got %v", err)
	}
}

func TestEnumConfigValidationAndNormalize(t *testing.T) {
	tu := New()

	if _, err := tu.Enum("e0", "a"); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
	if _, err := tu.Enum("e1", "a", WithEnumAllowed("a", "a")); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
	if _, err := tu.Enum("e2", "x", WithEnumAllowed("a", "b")); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}

	norm := func(s string) (string, bool) {
		switch s {
		case "A", "a":
			return "a", true
		case "B", "b":
			return "b", true
		default:
			return "", false
		}
	}
	e, err := tu.Enum("e3", "A", WithEnumAllowed("a", "b"), WithEnumNormalize(norm))
	if err != nil {
		t.Fatal(err)
	}
	if got := e.Get(); got != "a" {
		t.Fatalf("expected normalized default 'a', got %q", got)
	}
	if err := e.Set("B"); err != nil {
		t.Fatal(err)
	}
	if got := e.Get(); got != "b" {
		t.Fatalf("expected 'b', got %q", got)
	}
	if err := e.Set("C"); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("expected ErrInvalidValue, got %v", err)
	}
}

func TestExportOverridesOnlyNonDefault(t *testing.T) {
	tu := New()
	b, _ := tu.Bool("b", false)
	_ = b.Set(true)

	ov := tu.ExportOverrides()
	if len(ov) != 1 {
		t.Fatalf("expected 1 override, got %d", len(ov))
	}
	if ov[0].Key != "b" || ov[0].Type != TypeBool || ov[0].Value != "true" {
		t.Fatalf("unexpected override: %+v", ov[0])
	}

	_ = b.Set(false)
	ov = tu.ExportOverrides()
	if len(ov) != 0 {
		t.Fatalf("expected 0 overrides, got %d", len(ov))
	}
}

func TestRedactCoversValueAndDefault(t *testing.T) {
	tu := New()
	secret, err := tu.String("secret", "dflt", WithRedactString())
	if err != nil {
		t.Fatal(err)
	}
	_ = secret.Set("runtime")

	snap := tu.Snapshot()
	var it Item
	for _, x := range snap.Items {
		if x.Key == "secret" {
			it = x
			break
		}
	}
	if it.Key != "secret" {
		t.Fatalf("missing item secret in snapshot: %+v", snap.Items)
	}
	if it.Value != "<redacted>" || it.DefaultValue != "<redacted>" {
		t.Fatalf("expected redacted value+default, got value=%v default=%v", it.Value, it.DefaultValue)
	}

	ov := tu.ExportOverrides()
	if len(ov) != 1 || ov[0].Key != "secret" || ov[0].Value != "<redacted>" {
		t.Fatalf("expected redacted override, got %+v", ov)
	}
}

func TestSourceAndLastUpdatedAtSemantics(t *testing.T) {
	tu := New()
	b, err := tu.Bool("b", false)
	if err != nil {
		t.Fatal(err)
	}
	if !b.LastUpdatedAt().IsZero() {
		t.Fatalf("expected zero LastUpdatedAt initially, got %v", b.LastUpdatedAt())
	}
	if got := b.Source(); got != SourceDefault {
		t.Fatalf("expected SourceDefault initially, got %v", got)
	}

	_ = b.Set(true)
	if b.LastUpdatedAt().IsZero() {
		t.Fatalf("expected non-zero LastUpdatedAt after Set")
	}
	if got := b.Source(); got != SourceRuntimeSet {
		t.Fatalf("expected SourceRuntimeSet after non-default Set, got %v", got)
	}

	_ = b.Set(false)
	if got := b.Source(); got != SourceDefault {
		t.Fatalf("expected SourceDefault after Set back to default, got %v", got)
	}
}

func TestSetFromStringBoolLoose(t *testing.T) {
	tu := New()
	b, _ := tu.Bool("b", false)

	if err := tu.SetFromString("b", "TRUE"); err != nil {
		t.Fatal(err)
	}
	if got := b.Get(); got != true {
		t.Fatalf("expected true, got %v", got)
	}
	if err := tu.SetFromString("b", "0"); err != nil {
		t.Fatal(err)
	}
	if got := b.Get(); got != false {
		t.Fatalf("expected false, got %v", got)
	}
}

func TestSetFromStringParseFailuresByType(t *testing.T) {
	tu := New()
	_, _ = tu.Bool("b", false)
	_, _ = tu.Int64("i", 0)
	_, _ = tu.Float64("f", 0)
	_, _ = tu.String("s", "x")
	_, _ = tu.Duration("d", 0)
	_, _ = tu.Enum("e", "a", WithEnumAllowed("a", "b"))

	if err := tu.SetFromString("b", "maybe"); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("expected ErrInvalidValue for bool, got %v", err)
	}
	if err := tu.SetFromString("i", "not-a-number"); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("expected ErrInvalidValue for int64, got %v", err)
	}
	if err := tu.SetFromString("f", "nope"); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("expected ErrInvalidValue for float64, got %v", err)
	}
	if err := tu.SetFromString("d", "not-a-duration"); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("expected ErrInvalidValue for duration, got %v", err)
	}
	if err := tu.SetFromString("e", "c"); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("expected ErrInvalidValue for enum, got %v", err)
	}
	// Strings accept any value.
	if err := tu.SetFromString("s", ""); err != nil {
		t.Fatalf("expected nil for string, got %v", err)
	}
}

func TestSetAnyTypeMismatch(t *testing.T) {
	tu := New()
	_, _ = tu.Bool("b", false)
	_, _ = tu.Int64("i", 0)
	_, _ = tu.Duration("d", 0)
	_, _ = tu.Enum("e", "a", WithEnumAllowed("a", "b"))

	if err := tu.SetAny("b", int64(1)); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("expected ErrTypeMismatch, got %v", err)
	}
	if err := tu.SetAny("i", true); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("expected ErrTypeMismatch, got %v", err)
	}
	if err := tu.SetAny("d", "1s"); !errors.Is(err, ErrTypeMismatch) {
		t.Fatalf("expected ErrTypeMismatch, got %v", err)
	}
	// Enum expects string (so this is fine).
	if err := tu.SetAny("e", "b"); err != nil {
		t.Fatalf("expected nil for enum string, got %v", err)
	}
	// Enum string still validates allowed list.
	if err := tu.SetAny("e", "c"); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("expected ErrInvalidValue for enum value, got %v", err)
	}
}

func TestReentrantWriteDetected(t *testing.T) {
	tu := New()

	var innerErr atomic.Value
	b, err := tu.Bool("b", false, WithOnChangeBool(func(bool) {
		innerErr.Store(tu.SetFromString("b", "true"))
	}))
	if err != nil {
		t.Fatal(err)
	}

	if err := b.Set(true); err != nil {
		t.Fatalf("outer Set should succeed, got %v", err)
	}
	if got, _ := innerErr.Load().(error); !errors.Is(got, ErrReentrantWrite) {
		t.Fatalf("expected ErrReentrantWrite, got %v", got)
	}
}

func TestConcurrentReadWrite(t *testing.T) {
	tu := New()
	b, err := tu.Bool("b", false)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 10000; i++ {
			_ = b.Get()
		}
	}()

	for i := 0; i < 1000; i++ {
		_ = b.Set(i%2 == 0)
	}
	<-done
}

func TestSnapshotSortedByKey(t *testing.T) {
	tu := New()
	_, _ = tu.Bool("b", false)
	_, _ = tu.Bool("a", false)

	snap := tu.Snapshot()
	if len(snap.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(snap.Items))
	}
	if snap.Items[0].Key != "a" || snap.Items[1].Key != "b" {
		t.Fatalf("items not sorted: %+v", snap.Items)
	}
}

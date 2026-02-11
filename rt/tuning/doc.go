// Package tuning provides runtime-tunable parameters for online hot changes.
//
// It is designed to be small, stable and standard-library flavored:
// it only depends on the Go standard library,
// and it tries to keep semantics explicit with minimal "magic".
//
// # Design highlights
//
//   - Strong-typed handles: BoolVar / Int64Var / Float64Var / StringVar / DurationVar / EnumVar.
//   - Read path (Get) is lock-free, allocation-free and non-blocking.
//   - Write path (Set / Reset*) is thread-safe and blocking.
//   - The zero value of Tuning is ready to use.
//
// # Callback semantics (onChange)
//
// Each variable can register one or more onChange callbacks via WithOnChange* options.
//
// Callbacks are executed synchronously on the write path:
//   - A write API (Set / ResetToDefault / ResetToLastValue) first applies the new value,
//     then invokes callbacks.
//   - Callbacks are executed serially (one after another), in the order they were registered.
//   - Callback panics are recovered and swallowed. If the value write succeeded, Set still succeeds.
//   - Callbacks run even if the new value equals the current value.
//
// Concurrency and ordering:
//   - All write APIs in a Tuning instance are serialized (including callbacks).
//     If multiple goroutines call Set concurrently (even for different keys), only one write runs
//     at a time; others block until it completes.
//   - Get is lock-free and does not wait for writes.
//
// Important: callbacks must be fast and must not block (no I/O, no sleep, no waiting), because a
// slow callback blocks the current Set and all other writes in the same Tuning instance.
//
// # Quick start
//
//	tu := tuning.New()
//	enabled, _ := tu.Bool("feature.x", false)
//
//	_ = tu.SetFromString("feature.x", "on")
//	_ = enabled.Get()
//
// Callback panic visibility:
//
// tuning intentionally does NOT log/print on callback panics, to avoid introducing unexpected
// blocking and noise on the Set path. If you care about panic visibility, wrap your callback:
//
//	tuning.WithOnChangeBool(func(v bool) {
//		defer func() {
//			if r := recover(); r != nil {
//				// report it (slog / fmt.Fprintf(os.Stderr, ...) / metrics / ...)
//			}
//		}()
//		// ... your logic ...
//	})
//
// # Key rules
//
// Keys must be non-empty and can only contain characters in [A-Za-z0-9._-].
// '/' and whitespace are not allowed.
//
// Keys are case-sensitive: "Foo" and "foo" are different keys.
//
// # Source / default
//
// Source indicates where the current effective value comes from:
//   - SourceDefault: current value equals the registered default value
//   - SourceRuntimeSet: current value differs from the registered default value
//
// This means Source reflects the current effective state (not historical actions).
//
// # LastUpdatedAt
//
// LastUpdatedAt is zero until the first successful runtime write (Set/Reset*).
//
// # Re-entrant writes (important)
//
// onChange callbacks MUST NOT call tuning write APIs (Set / Reset*).
// Doing so is a programming error. tuning detects re-entrant writes and returns ErrReentrantWrite.
//
// # SetFromString parsing
//
// Tuning provides SetFromString for ops/admin usage. For bool values it is intentionally
// slightly lenient and accepts common forms (case-insensitive): true/false, t/f, 1/0,
// yes/no, y/n, on/off.
//
// # SetAny
//
// SetAny allows setting a value from typed Go values (bool/int64/float64/string/duration).
// If the input type does not match the registered variable type, SetAny returns ErrTypeMismatch.
//
// # Key-based helpers
//
// tuning is designed around strong-typed variable handles (BoolVar/Int64Var/...), but some
// ops/admin workflows only have a key string at runtime. For these cases, Tuning also provides:
//   - Lookup(key) (Item, bool): a point-in-time view for a single key (redaction rules apply)
//   - ResetToDefault(key) error: reset a key back to its registered default value
//   - ResetToLastValue(key) error: undo one step for a key (ErrNoLastValue if none)
//
// Redaction:
// If a variable is registered with WithRedact*, Snapshot/Lookup and ExportOverrides will replace
// its Value/DefaultValue (and override Value) with "<redacted>".
package tuning

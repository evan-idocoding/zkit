package safego

import "context"

// Tag is a lightweight key/value pair carried by panic/error reports.
// Tags are kept as a slice to preserve insertion order for stable output.
type Tag struct {
	Key   string
	Value string
}

// ErrorHandler is called when a function returns a non-nil error (subject to filtering).
type ErrorHandler func(ctx context.Context, info ErrorInfo)

// ErrorInfo describes an error returned from a function.
type ErrorInfo struct {
	Name string
	Tags []Tag
	Err  error
}

// PanicHandler is called when a function panics (subject to policy).
type PanicHandler func(ctx context.Context, info PanicInfo)

// PanicInfo describes a recovered panic.
type PanicInfo struct {
	Name  string
	Tags  []Tag
	Value any
	Stack []byte
}

// PanicPolicy controls how panics are handled.
type PanicPolicy int

const (
	// RecoverAndReport recovers the panic and reports it via PanicHandler (or stderr by default).
	RecoverAndReport PanicPolicy = iota
	// RecoverOnly recovers the panic without reporting it.
	RecoverOnly
	// RepanicAfterReport recovers the panic, reports it, then panics again with the same value.
	RepanicAfterReport
)

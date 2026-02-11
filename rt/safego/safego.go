package safego

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
)

// Go starts fn in a new goroutine, applying the configured panic/error handling.
//
// Any error returned by fn (if any) is reported via WithErrorHandler (or stderr by default).
func Go(ctx context.Context, fn func(context.Context), opts ...Option) {
	GoErr(ctx, func(ctx context.Context) error {
		fn(ctx)
		return nil
	}, opts...)
}

// GoErr starts fn in a new goroutine, applying the configured panic/error handling.
//
// The error returned by fn is not returned to the caller. Instead, it is reported via
// WithErrorHandler (or stderr by default, subject to filtering such as context cancellation).
func GoErr(ctx context.Context, fn func(context.Context) error, opts ...Option) {
	go RunErr(ctx, fn, opts...)
}

// Run executes fn synchronously (it does not start a goroutine), applying the configured panic/error handling.
//
// If you want to start your own goroutine (e.g. with custom scheduling), call Run/RunErr inside it.
func Run(ctx context.Context, fn func(context.Context), opts ...Option) {
	RunErr(ctx, func(ctx context.Context) error {
		fn(ctx)
		return nil
	}, opts...)
}

// RunErr executes fn synchronously (it does not start a goroutine), applying the configured panic/error handling.
//
// The error returned by fn is not returned to the caller. Instead, it is reported via
// WithErrorHandler (or stderr by default, subject to filtering such as context cancellation).
//
// If ctx is nil, it is treated as context.Background().
func RunErr(ctx context.Context, fn func(context.Context) error, opts ...Option) {
	if ctx == nil {
		ctx = context.Background()
	}

	c := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&c)
		}
	}

	// Always run finalizers (LIFO), even when we repanic.
	defer runFinalizers(ctx, c)

	defer func() {
		p := recover()
		if p == nil {
			return
		}

		switch c.panicPolicy {
		case RecoverOnly:
			// swallow
			return
		case RecoverAndReport, RepanicAfterReport:
			info := PanicInfo{
				Name:  c.name,
				Tags:  cloneTags(c.tags),
				Value: p,
				Stack: debug.Stack(),
			}

			if c.onPanic != nil {
				callPanicHandlerNoPanic(ctx, c.onPanic, info)
			} else {
				reportPanicToStderr(info)
			}

			if c.panicPolicy == RepanicAfterReport {
				panic(p)
			}
			return
		default:
			// Unknown policy: be conservative and recover+report.
			info := PanicInfo{
				Name:  c.name,
				Tags:  cloneTags(c.tags),
				Value: p,
				Stack: debug.Stack(),
			}
			if c.onPanic != nil {
				callPanicHandlerNoPanic(ctx, c.onPanic, info)
			} else {
				reportPanicToStderr(info)
			}
			return
		}
	}()

	err := fn(ctx)
	if err == nil {
		return
	}
	if !c.reportContextCancel && isContextCancel(err) {
		return
	}

	info := ErrorInfo{
		Name: c.name,
		Tags: cloneTags(c.tags),
		Err:  err,
	}
	if c.onError != nil {
		callErrorHandlerNoPanic(ctx, c.onError, info)
		return
	}
	reportErrorToStderr(info)
}

func runFinalizers(ctx context.Context, c config) {
	// LIFO, like defer.
	for i := len(c.finally) - 1; i >= 0; i-- {
		fn := c.finally[i]
		if fn == nil {
			continue
		}
		func() {
			defer func() {
				p := recover()
				if p == nil {
					return
				}

				// Finalizer panic should not take down the goroutine/process,
				// but it must be observable for debugging.
				info := PanicInfo{
					Name:  c.name,
					Tags:  cloneTags(c.tags),
					Value: fmt.Sprintf("safego: finalizer panicked: %v", p),
					Stack: debug.Stack(),
				}

				if c.onPanic != nil {
					callPanicHandlerNoPanic(ctx, c.onPanic, info)
				} else {
					reportPanicToStderr(info)
				}
			}()
			fn()
		}()
	}
}

func cloneTags(tags []Tag) []Tag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]Tag, len(tags))
	copy(out, tags)
	return out
}

func isContextCancel(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func callErrorHandlerNoPanic(ctx context.Context, h ErrorHandler, info ErrorInfo) {
	defer func() {
		if p := recover(); p != nil {
			// Avoid secondary panics from user handlers taking down the program.
			reportPanicToStderr(PanicInfo{
				Name:  info.Name,
				Tags:  info.Tags,
				Value: fmt.Sprintf("safego: error handler panicked: %v", p),
				Stack: debug.Stack(),
			})
		}
	}()
	h(ctx, info)
}

func callPanicHandlerNoPanic(ctx context.Context, h PanicHandler, info PanicInfo) {
	defer func() {
		if p := recover(); p != nil {
			// Avoid secondary panics from user handlers taking down the program.
			reportPanicToStderr(PanicInfo{
				Name:  info.Name,
				Tags:  info.Tags,
				Value: fmt.Sprintf("safego: panic handler panicked: %v", p),
				Stack: debug.Stack(),
			})
		}
	}()
	h(ctx, info)
}

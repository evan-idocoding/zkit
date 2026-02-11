package safego

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestRunErr_FinallyRunsOnSuccess(t *testing.T) {
	t.Parallel()

	var called atomic.Int64
	RunErr(context.Background(), func(context.Context) error {
		return nil
	}, WithFinally(func() { called.Add(1) }))

	if got := called.Load(); got != 1 {
		t.Fatalf("finally called=%d, want 1", got)
	}
}

func TestRunErr_FinallyRunsOnPanic_RecoverAndReport(t *testing.T) {
	t.Parallel()

	var finally atomic.Int64
	var panicCalls atomic.Int64

	RunErr(context.Background(), func(context.Context) error {
		panic("boom")
	}, WithFinally(func() { finally.Add(1) }),
		WithPanicPolicy(RecoverAndReport),
		WithPanicHandler(func(context.Context, PanicInfo) { panicCalls.Add(1) }),
	)

	if got := finally.Load(); got != 1 {
		t.Fatalf("finally called=%d, want 1", got)
	}
	if got := panicCalls.Load(); got != 1 {
		t.Fatalf("panic handler called=%d, want 1", got)
	}
}

func TestRunErr_FinallyRunsOnPanic_RepanicAfterReport(t *testing.T) {
	t.Parallel()

	var finally atomic.Int64
	var panicCalls atomic.Int64

	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic to propagate")
		}
		if got := finally.Load(); got != 1 {
			t.Fatalf("finally called=%d, want 1", got)
		}
		if got := panicCalls.Load(); got != 1 {
			t.Fatalf("panic handler called=%d, want 1", got)
		}
	}()

	RunErr(context.Background(), func(context.Context) error {
		panic("boom")
	}, WithFinally(func() { finally.Add(1) }),
		WithPanicPolicy(RepanicAfterReport),
		WithPanicHandler(func(context.Context, PanicInfo) { panicCalls.Add(1) }),
	)
}

func TestRunErr_ErrorHandler_DefaultIgnoreCancel(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	RunErr(context.Background(), func(context.Context) error {
		return context.Canceled
	}, WithErrorHandler(func(context.Context, ErrorInfo) { calls.Add(1) }))

	if got := calls.Load(); got != 0 {
		t.Fatalf("error handler called=%d, want 0", got)
	}
}

func TestRunErr_ErrorHandler_ReportCancel(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64

	RunErr(context.Background(), func(context.Context) error {
		return context.DeadlineExceeded
	}, WithReportContextCancel(true),
		WithErrorHandler(func(context.Context, ErrorInfo) { calls.Add(1) }),
	)

	if got := calls.Load(); got != 1 {
		t.Fatalf("error handler called=%d, want 1", got)
	}
}

func TestRunErr_ErrorHandler_CalledWithNameTags(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("x")

	var gotName string
	var gotTags []Tag
	var gotErr error

	RunErr(context.Background(), func(context.Context) error {
		return wantErr
	}, WithName("n"),
		WithTags(Tag{Key: "k", Value: "v"}),
		WithReportContextCancel(true),
		WithErrorHandler(func(_ context.Context, info ErrorInfo) {
			gotName = info.Name
			gotTags = info.Tags
			gotErr = info.Err
		}),
	)

	if gotName != "n" {
		t.Fatalf("name=%q, want %q", gotName, "n")
	}
	if len(gotTags) != 1 || gotTags[0].Key != "k" || gotTags[0].Value != "v" {
		t.Fatalf("tags=%v, want [{k v}]", gotTags)
	}
	if !errors.Is(gotErr, wantErr) {
		t.Fatalf("err=%v, want %v", gotErr, wantErr)
	}
}

func TestRunErr_FinallyOrderIsLIFO(t *testing.T) {
	t.Parallel()

	var seq atomic.Int64
	var a atomic.Int64
	var b atomic.Int64

	RunErr(context.Background(), func(context.Context) error { return nil },
		WithFinally(func() { a.Store(seq.Add(1)) }),
		WithFinally(func() { b.Store(seq.Add(1)) }),
	)

	// LIFO: second WithFinally runs first.
	if a.Load() != 2 || b.Load() != 1 {
		t.Fatalf("finally order: a=%d b=%d; want a=2 b=1 (LIFO)", a.Load(), b.Load())
	}
}

func TestRunErr_NilContextIsAllowed(t *testing.T) {
	t.Parallel()

	var sawNonNil atomic.Bool
	RunErr(nil, func(ctx context.Context) error {
		if ctx != nil {
			sawNonNil.Store(true)
		}
		return nil
	})
	if !sawNonNil.Load() {
		t.Fatalf("expected ctx to be non-nil inside fn")
	}
}

func TestRunErr_ErrorHandlerPanicIsContained(t *testing.T) {
	t.Parallel()

	RunErr(context.Background(), func(context.Context) error {
		return errors.New("x")
	}, WithReportContextCancel(true),
		WithErrorHandler(func(context.Context, ErrorInfo) { panic("handler boom") }),
	)
}

func TestRunErr_PanicHandlerPanicIsContained(t *testing.T) {
	t.Parallel()

	RunErr(context.Background(), func(context.Context) error {
		panic("boom")
	}, WithPanicPolicy(RecoverAndReport),
		WithPanicHandler(func(context.Context, PanicInfo) { panic("handler boom") }),
	)
}

func TestRunErr_FinalizerPanicIsContainedAndReported(t *testing.T) {
	t.Parallel()

	var panicCalls atomic.Int64

	RunErr(context.Background(), func(context.Context) error {
		return nil
	}, WithPanicHandler(func(context.Context, PanicInfo) {
		panicCalls.Add(1)
	}), WithFinally(func() {
		panic("finalizer boom")
	}))

	if got := panicCalls.Load(); got != 1 {
		t.Fatalf("panic handler called=%d, want 1", got)
	}
}

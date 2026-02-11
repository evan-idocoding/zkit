// Package safego provides helpers for running functions with panic/error reporting.
//
// safego is intentionally small and standard-library flavored: it only depends on the Go
// standard library, and it tries to make failures from background goroutines observable
// by default.
//
// safego does not return errors to its caller. Instead, errors and panics are reported via
// handlers (if configured) or to stderr (by default). This makes it suitable for
// fire-and-forget background work where you still want failures to be noticed.
//
// # Synchronous vs asynchronous
//
// Go/GoErr start a new goroutine. Run/RunErr execute synchronously (they do not start a goroutine).
// In all cases, errors returned from the function are reported; they are not returned to the caller.
//
// Nil context: if ctx is nil, safego treats it as context.Background().
//
// # WaitGroup integration
//
// A common pattern is to combine safego with sync.WaitGroup, using WithFinally to avoid
// forgetting wg.Done() even when panic happens.
//
//	wg.Add(1)
//	safego.GoErr(ctx, work,
//		safego.WithName("cache-refresh"),
//		safego.WithFinally(wg.Done),
//	)
//
// If you prefer to control goroutine creation yourself, use Run/RunErr inside your own goroutine:
//
//	wg.Add(1)
//	go func() {
//		defer wg.Done()
//		safego.RunErr(ctx, work, safego.WithName("cache-refresh"))
//	}()
//
// Note: safego does NOT make WaitGroup safe to misuse. In particular, do NOT call wg.Add
// concurrently with wg.Wait. A typical shutdown order is:
// stop scheduling new tasks → cancel context → wg.Wait().
//
// # Error reporting
//
// Errors returned by GoErr/RunErr are reported via WithErrorHandler if provided, otherwise to stderr.
//
// By default, context.Canceled and context.DeadlineExceeded are NOT reported because they are
// common during shutdown. Use WithReportContextCancel(true) to report them.
//
// # Panic policy
//
// By default, safego uses RecoverAndReport: it recovers panics and reports them via WithPanicHandler
// if provided, otherwise to stderr.
//
// If you prefer "fail fast" semantics, use RepanicAfterReport to report and then panic again.
// Use RecoverOnly to recover without reporting.
//
// # Finalizers
//
// WithFinally functions are always executed (on success, error, panic, and repanic), in LIFO order.
// If a finalizer panics, the panic is contained (not rethrown) but reported (handler or stderr).
//
// # Notes on stderr reporting
//
// Writing to stderr can block in extreme environments. If you are latency-sensitive or expect
// high-frequency errors/panics, provide handlers that implement your desired strategy.
// The exact stderr output format should be treated as best-effort diagnostic output and may change.
package safego

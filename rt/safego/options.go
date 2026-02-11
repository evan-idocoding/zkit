package safego

type config struct {
	name string
	tags []Tag

	finally []func()

	onError             ErrorHandler
	reportContextCancel bool

	onPanic     PanicHandler
	panicPolicy PanicPolicy
}

// Option configures a single Go/GoErr/Run/RunErr call.
type Option func(*config)

func defaultConfig() config {
	return config{
		panicPolicy:         RecoverAndReport,
		reportContextCancel: false,
	}
}

// WithName sets a human-friendly name for the goroutine/task.
func WithName(name string) Option {
	return func(c *config) { c.name = name }
}

// WithTag appends a single tag (key/value) to reports.
func WithTag(key, value string) Option {
	return func(c *config) {
		c.tags = append(c.tags, Tag{Key: key, Value: value})
	}
}

// WithTags appends tags to reports (preserving order).
func WithTags(tags ...Tag) Option {
	return func(c *config) {
		if len(tags) == 0 {
			return
		}
		c.tags = append(c.tags, tags...)
	}
}

// WithFinally registers a function to be called when execution finishes.
//
// Finalizers are executed in LIFO order (like defer).
//
// If a finalizer panics, the panic is recovered and reported (handler or stderr). It is not rethrown.
func WithFinally(fn func()) Option {
	return func(c *config) {
		if fn == nil {
			return
		}
		c.finally = append(c.finally, fn)
	}
}

// WithErrorHandler sets the error handler.
//
// If not set, errors are reported to stderr by default. Panics in the handler are contained:
// they are recovered and reported to stderr.
func WithErrorHandler(h ErrorHandler) Option {
	return func(c *config) { c.onError = h }
}

// WithReportContextCancel controls whether context cancellation errors are reported.
//
// By default, context.Canceled and context.DeadlineExceeded are NOT reported because they are
// very common in long-running background tasks.
func WithReportContextCancel(report bool) Option {
	return func(c *config) { c.reportContextCancel = report }
}

// WithPanicHandler sets the panic handler. If not set, panics are reported to stderr by default
// (unless the policy is RecoverOnly).
//
// Panics in the handler are contained: they are recovered and reported to stderr.
func WithPanicHandler(h PanicHandler) Option {
	return func(c *config) { c.onPanic = h }
}

// WithPanicPolicy sets the panic handling policy.
func WithPanicPolicy(p PanicPolicy) Option {
	return func(c *config) { c.panicPolicy = p }
}

package task

import (
	"time"

	"github.com/evan-idocoding/zkit/rt/safego"
)

type taskConfig struct {
	name string
	tags []safego.Tag

	fn Func

	overlap       OverlapPolicy
	maxConcurrent int

	// Every-only
	interval         time.Duration
	everyMode        EveryMode
	startImmediately bool

	// safego-style handlers
	onError             safego.ErrorHandler
	onPanic             safego.PanicHandler
	reportContextCancel bool

	// hooks
	onRunStart  func(info RunStartInfo)
	onRunFinish func(info RunFinishInfo)
}

type Option func(*taskConfig)

// WithName sets a human-friendly task name.
//
// Notes:
//   - Name is optional (empty means unnamed).
//   - Name is normalized by strings.TrimSpace.
//   - Non-empty names must match [A-Za-z0-9._-].
//   - Non-empty names are unique within a Manager; Add returns ErrDuplicateName on duplicates.
func WithName(name string) Option {
	return func(c *taskConfig) { c.name = name }
}

// WithTags appends tags for error/panic reports and hooks.
func WithTags(tags ...safego.Tag) Option {
	return func(c *taskConfig) {
		if len(tags) == 0 {
			return
		}
		c.tags = append(c.tags, tags...)
	}
}

// WithMaxConcurrent sets the max concurrent runs for a task.
//
// If n <= 0, Add panics (configuration error).
func WithMaxConcurrent(n int) Option {
	return func(c *taskConfig) { c.maxConcurrent = n }
}

// WithOverlapPolicy sets how run opportunities are handled under contention.
func WithOverlapPolicy(p OverlapPolicy) Option {
	return func(c *taskConfig) { c.overlap = p }
}

// WithStartImmediately controls whether an Every task runs immediately upon Start/Add.
// Default is false.
func WithStartImmediately(v bool) Option {
	return func(c *taskConfig) { c.startImmediately = v }
}

// WithEveryMode sets the scheduling mode for an Every task.
// Default is EveryFixedDelay.
func WithEveryMode(mode EveryMode) Option {
	return func(c *taskConfig) { c.everyMode = mode }
}

// WithErrorHandler sets the error handler. If not set, errors are reported to stderr by default.
func WithErrorHandler(h safego.ErrorHandler) Option {
	return func(c *taskConfig) { c.onError = h }
}

// WithPanicHandler sets the panic handler. If not set, panics are reported to stderr by default.
func WithPanicHandler(h safego.PanicHandler) Option {
	return func(c *taskConfig) { c.onPanic = h }
}

// WithReportContextCancel controls whether context.Canceled and context.DeadlineExceeded are reported.
func WithReportContextCancel(report bool) Option {
	return func(c *taskConfig) { c.reportContextCancel = report }
}

// WithOnRunStart sets a hook to observe run starts. Hooks are called synchronously.
func WithOnRunStart(fn func(info RunStartInfo)) Option {
	return func(c *taskConfig) { c.onRunStart = fn }
}

// WithOnRunFinish sets a hook to observe run finishes. Hooks are called synchronously.
func WithOnRunFinish(fn func(info RunFinishInfo)) Option {
	return func(c *taskConfig) { c.onRunFinish = fn }
}

type managerConfig struct {
	onRunStart  func(info RunStartInfo)
	onRunFinish func(info RunFinishInfo)

	onError             safego.ErrorHandler
	onPanic             safego.PanicHandler
	reportContextCancel bool
}

type ManagerOption func(*managerConfig)

// WithManagerOnRunStart sets a global hook for all tasks in this manager.
func WithManagerOnRunStart(fn func(info RunStartInfo)) ManagerOption {
	return func(c *managerConfig) { c.onRunStart = fn }
}

// WithManagerOnRunFinish sets a global hook for all tasks in this manager.
func WithManagerOnRunFinish(fn func(info RunFinishInfo)) ManagerOption {
	return func(c *managerConfig) { c.onRunFinish = fn }
}

// WithManagerErrorHandler sets a default error handler for tasks added to this manager.
func WithManagerErrorHandler(h safego.ErrorHandler) ManagerOption {
	return func(c *managerConfig) { c.onError = h }
}

// WithManagerPanicHandler sets a default panic handler for tasks added to this manager.
func WithManagerPanicHandler(h safego.PanicHandler) ManagerOption {
	return func(c *managerConfig) { c.onPanic = h }
}

// WithManagerReportContextCancel sets the default reportContextCancel for tasks added to this manager.
func WithManagerReportContextCancel(report bool) ManagerOption {
	return func(c *managerConfig) { c.reportContextCancel = report }
}

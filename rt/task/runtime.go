package task

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/evan-idocoding/zkit/rt/safego"
)

type runResult struct {
	err      error
	panicked bool
}

type runWaiter chan runResult

type taskRuntime struct {
	m *Manager

	kind taskKind
	fn   Func

	name string
	tags []safego.Tag

	overlap       OverlapPolicy
	maxConcurrent int

	// every-only
	interval         time.Duration
	everyMode        EveryMode
	startImmediately bool

	// scheduling internals
	baseTime time.Time
	nextRun  time.Time
	doneCh   chan struct{} // closed on each run completion; replaced with a new channel (EveryFixedDelay)

	// safego-style handlers
	onError             safego.ErrorHandler
	onPanic             safego.PanicHandler
	reportContextCancel bool

	// hooks: both manager-global and task-local (both run)
	onRunStartGlobal  func(info RunStartInfo)
	onRunFinishGlobal func(info RunFinishInfo)
	onRunStartLocal   func(info RunStartInfo)
	onRunFinishLocal  func(info RunFinishInfo)

	mu sync.Mutex

	state   State
	running int
	pending bool

	pendingWaiters []runWaiter

	runCount      uint64
	failCount     uint64
	successCount  uint64
	canceledCount uint64

	lastStarted  time.Time
	lastFinished time.Time
	lastSuccess  time.Time
	lastDuration time.Duration
	lastError    string

	// scheduler started?
	schedOnce sync.Once
}

func taskConfigFrom(m *Manager, t Task, opts []Option) taskConfig {
	c := defaultTaskConfig(m, t)
	for _, opt := range opts {
		if opt != nil {
			opt(&c)
		}
	}
	return c
}

func newTaskRuntimeFromConfig(m *Manager, t Task, c taskConfig) *taskRuntime {
	if c.maxConcurrent <= 0 {
		panic(fmt.Sprintf("task: WithMaxConcurrent(%d) is invalid (must be > 0)", c.maxConcurrent))
	}
	if c.fn == nil {
		panic("task: task Func is nil")
	}
	if t.kind() == taskKindEvery && c.interval <= 0 {
		panic(fmt.Sprintf("task: Every interval=%s is invalid (must be > 0)", c.interval))
	}

	tr := &taskRuntime{
		m:                   m,
		kind:                t.kind(),
		fn:                  c.fn,
		name:                c.name,
		tags:                cloneTags(c.tags),
		overlap:             c.overlap,
		maxConcurrent:       c.maxConcurrent,
		interval:            c.interval,
		everyMode:           c.everyMode,
		startImmediately:    c.startImmediately,
		onError:             c.onError,
		onPanic:             c.onPanic,
		reportContextCancel: c.reportContextCancel,
		onRunStartGlobal:    m.cfg.onRunStart,
		onRunFinishGlobal:   m.cfg.onRunFinish,
		onRunStartLocal:     c.onRunStart,
		onRunFinishLocal:    c.onRunFinish,
		doneCh:              make(chan struct{}),
		state:               StateNotStarted,
	}
	return tr
}

func defaultTaskConfig(m *Manager, t Task) taskConfig {
	c := taskConfig{
		maxConcurrent: 1,
		everyMode:     EveryFixedDelay,

		onError:             m.cfg.onError,
		onPanic:             m.cfg.onPanic,
		reportContextCancel: m.cfg.reportContextCancel,
	}

	switch tt := t.(type) {
	case triggerTask:
		c.fn = tt.fn
		c.overlap = OverlapMerge
	case everyTask:
		c.fn = tt.fn
		c.interval = tt.interval
		c.overlap = OverlapSkip
	default:
		// unknown Task implementation; rely on kind() only
		switch t.kind() {
		case taskKindTrigger:
			c.overlap = OverlapMerge
		case taskKindEvery:
			c.overlap = OverlapSkip
		}
	}
	return c
}

func cloneTags(tags []safego.Tag) []safego.Tag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]safego.Tag, len(tags))
	copy(out, tags)
	return out
}

func (tr *taskRuntime) Name() string { return tr.name }

func (tr *taskRuntime) Trigger() { _ = tr.TryTrigger() }

func (tr *taskRuntime) TryTrigger() bool {
	accepted, _ := tr.requestRun(RunKindTrigger, time.Time{}, nil)
	return accepted
}

func (tr *taskRuntime) TriggerAndWait(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ch := make(runWaiter, 1)
	_, err := tr.requestRun(RunKindTrigger, time.Time{}, ch)
	if err != nil {
		return err
	}

	select {
	case res := <-ch:
		if res.panicked {
			return ErrPanicked
		}
		return res.err
	case <-ctx.Done():
		// Best-effort cleanup: if this waiter was enqueued as a pending waiter (OverlapMerge),
		// remove it to avoid unbounded growth when callers timeout/cancel.
		//
		// Note: We do NOT cancel the pending run opportunity; we only detach this waiter's interest.
		tr.removePendingWaiter(ch)
		return ctx.Err()
	}
}

func (tr *taskRuntime) Status() Status {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	st := Status{
		Name:  tr.name,
		Tags:  cloneTags(tr.tags),
		State: tr.state,

		Running: tr.running,
		Pending: tr.pending,

		RunCount:      tr.runCount,
		FailCount:     tr.failCount,
		SuccessCount:  tr.successCount,
		CanceledCount: tr.canceledCount,

		LastStarted:  tr.lastStarted,
		LastFinished: tr.lastFinished,
		LastSuccess:  tr.lastSuccess,
		LastDuration: tr.lastDuration,
		LastError:    tr.lastError,

		NextRun: tr.nextRun,
	}
	return st
}

func (tr *taskRuntime) onManagerStart(ctx context.Context, base time.Time) {
	tr.mu.Lock()
	if tr.state != StateNotStarted {
		tr.mu.Unlock()
		return
	}
	tr.state = StateIdle
	tr.baseTime = base
	tr.mu.Unlock()

	if tr.kind == taskKindEvery {
		tr.startSchedulerOnce(ctx)
	}
}

func (tr *taskRuntime) startSchedulerOnce(ctx context.Context) {
	tr.schedOnce.Do(func() {
		if ctx == nil {
			return
		}
		tr.m.wg.Add(1)
		go func() {
			defer tr.m.wg.Done()
			tr.runScheduler(ctx)
		}()
	})
}

func (tr *taskRuntime) requestRun(kind RunKind, scheduledAt time.Time, waiter runWaiter) (bool, error) {
	// Gate run starts with manager startMu:
	// - Start/Shutdown take Lock() to transition states
	// - starting a run takes RLock() to ensure no new run can start after Shutdown begins.
	tr.m.startMu.RLock()
	st := managerState(tr.m.state.Load())
	if st == managerNotStarted {
		tr.m.startMu.RUnlock()
		return false, ErrNotRunning
	}
	if st != managerRunning {
		tr.m.startMu.RUnlock()
		return false, ErrClosed
	}
	ctx := tr.m.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	accepted, err := tr.tryStartOrPend(ctx, kind, scheduledAt, waiter)
	tr.m.startMu.RUnlock()
	return accepted, err
}

func (tr *taskRuntime) tryStartOrPend(ctx context.Context, kind RunKind, scheduledAt time.Time, waiter runWaiter) (bool, error) {
	var waiters []runWaiter
	if waiter != nil {
		waiters = append(waiters, waiter)
	}

	tr.mu.Lock()
	if tr.state == StateStopping || tr.state == StateStopped {
		tr.mu.Unlock()
		return false, ErrClosed
	}
	if tr.state == StateNotStarted {
		tr.mu.Unlock()
		return false, ErrNotRunning
	}

	if tr.running < tr.maxConcurrent {
		// start now
		tr.running++
		tr.state = StateRunning
		tr.runCount++
		startedAt := time.Now()
		tr.lastStarted = startedAt
		startInfo := RunStartInfo{
			Name:        tr.name,
			Tags:        cloneTags(tr.tags),
			Kind:        kind,
			ScheduledAt: scheduledAt,
			StartedAt:   startedAt,
		}
		tr.mu.Unlock()

		tr.m.wg.Add(1)
		go func() {
			defer tr.m.wg.Done()
			tr.callOnRunStart(ctx, startInfo)
			tr.runOnce(ctx, kind, scheduledAt, startedAt, waiters)
		}()
		return true, nil
	}

	// At max concurrency.
	switch tr.overlap {
	case OverlapSkip:
		tr.mu.Unlock()
		return false, ErrSkipped
	case OverlapMerge:
		tr.pending = true
		if len(waiters) > 0 {
			tr.pendingWaiters = append(tr.pendingWaiters, waiters...)
		}
		tr.mu.Unlock()
		return true, nil
	default:
		tr.mu.Unlock()
		return false, ErrSkipped
	}
}

func (tr *taskRuntime) runOnce(ctx context.Context, kind RunKind, scheduledAt time.Time, startedAt time.Time, waiters []runWaiter) {
	var (
		err        error // exposed to TriggerAndWait (filtered cancels become nil)
		rawErr     error // unfiltered return value for status accounting
		filteredCC bool  // context cancel filtered by reportContextCancel=false
		panicked   bool
	)
	defer func() {
		var startPendingWaiters []runWaiter
		startPending := false
		if p := recover(); p != nil {
			panicked = true
			tr.reportPanic(ctx, p)
		}
		finishedAt := time.Now()
		dur := finishedAt.Sub(startedAt)

		tr.mu.Lock()
		tr.running--
		if tr.running < 0 {
			tr.running = 0
		}

		tr.lastFinished = finishedAt
		tr.lastDuration = dur

		if panicked {
			tr.failCount++
			tr.lastError = "panic"
		} else if rawErr != nil && !filteredCC {
			tr.failCount++
			tr.lastError = rawErr.Error()
		} else if rawErr == nil {
			tr.successCount++
			tr.lastSuccess = finishedAt
		} else {
			// filtered context cancel: neither success nor failure
			tr.canceledCount++
		}

		// If OverlapMerge has pending work and capacity is available, start it.
		if tr.pending && tr.running < tr.maxConcurrent && tr.m.managerState() == managerRunning {
			startPending = true
			tr.pending = false
			startPendingWaiters = tr.pendingWaiters
			tr.pendingWaiters = nil
		}

		// state transition
		if tr.m.managerState() == managerStopped {
			tr.state = StateStopped
		} else if tr.m.managerState() >= managerStopping {
			tr.state = StateStopping
		} else if tr.running > 0 {
			tr.state = StateRunning
		} else {
			tr.state = StateIdle
		}

		finishInfo := RunFinishInfo{
			Name:        tr.name,
			Tags:        cloneTags(tr.tags),
			Kind:        kind,
			ScheduledAt: scheduledAt,
			StartedAt:   startedAt,
			FinishedAt:  finishedAt,
			Duration:    dur,
			Err:         "",
			Panicked:    panicked,
		}
		if err != nil {
			finishInfo.Err = err.Error()
		}
		tr.rotateDoneChLocked()
		tr.mu.Unlock()

		tr.callOnRunFinish(ctx, finishInfo)

		// Deliver waiter results.
		res := runResult{err: err, panicked: panicked}
		for _, w := range waiters {
			if w == nil {
				continue
			}
			select {
			case w <- res:
			default:
			}
		}

		// Start merged pending run (outside lock).
		if startPending {
			tr.startMergedPending(startPendingWaiters)
		}
	}()

	// Execute function; any panic is recovered by deferred func above.
	rawErr = tr.fn(ctx)
	err = rawErr
	if rawErr != nil && tr.shouldReportError(rawErr) {
		tr.reportError(ctx, rawErr)
	} else if rawErr != nil {
		// filtered context cancellation: hide from TriggerAndWait and do not count as success/failure.
		filteredCC = errors.Is(rawErr, context.Canceled) || errors.Is(rawErr, context.DeadlineExceeded)
		err = nil
	}
}

func (tr *taskRuntime) shouldReportError(err error) bool {
	if err == nil {
		return false
	}
	if tr.reportContextCancel {
		return true
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

func (tr *taskRuntime) reportError(ctx context.Context, err error) {
	info := safego.ErrorInfo{
		Name: tr.name,
		Tags: cloneTags(tr.tags),
		Err:  err,
	}
	if tr.onError != nil {
		callErrorHandlerNoPanic(ctx, tr.onError, tr.name, tr.tags, info)
		return
	}
	reportErrorToStderr(tr.name, tr.tags, err)
}

func (tr *taskRuntime) reportPanic(ctx context.Context, p any) {
	if tr.onPanic != nil {
		stack := debug.Stack()
		info := safego.PanicInfo{
			Name:  tr.name,
			Tags:  cloneTags(tr.tags),
			Value: p,
			Stack: stack,
		}
		callPanicHandlerNoPanic(ctx, tr.onPanic, tr.name, tr.tags, info)
		return
	}
	reportPanicToStderr(tr.name, tr.tags, p, debug.Stack())
}

func (tr *taskRuntime) startMergedPending(waiters []runWaiter) {
	if len(waiters) == 0 {
		// still should start the merged run if possible
	}
	// Start merged pending under the same run-start gate as requestRun.
	tr.m.startMu.RLock()
	st := managerState(tr.m.state.Load())
	if st != managerRunning {
		tr.m.startMu.RUnlock()
		for _, w := range waiters {
			if w == nil {
				continue
			}
			select {
			case w <- runResult{err: ErrClosed}:
			default:
			}
		}
		return
	}
	ctx := tr.m.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	tr.mu.Lock()
	if tr.state == StateStopping || tr.state == StateStopped || tr.state == StateNotStarted {
		tr.mu.Unlock()
		tr.m.startMu.RUnlock()
		for _, w := range waiters {
			if w == nil {
				continue
			}
			select {
			case w <- runResult{err: ErrClosed}:
			default:
			}
		}
		return
	}

	if tr.running < tr.maxConcurrent {
		tr.running++
		tr.state = StateRunning
		tr.runCount++
		startedAt := time.Now()
		tr.lastStarted = startedAt
		startInfo := RunStartInfo{
			Name:        tr.name,
			Tags:        cloneTags(tr.tags),
			Kind:        RunKindTrigger,
			ScheduledAt: time.Time{},
			StartedAt:   startedAt,
		}
		tr.mu.Unlock()
		tr.m.startMu.RUnlock()

		tr.m.wg.Add(1)
		go func() {
			defer tr.m.wg.Done()
			tr.callOnRunStart(ctx, startInfo)
			tr.runOnce(ctx, RunKindTrigger, time.Time{}, startedAt, waiters)
		}()
		return
	}

	// Could not start due to concurrent triggers; merge back.
	tr.pending = true
	if len(waiters) > 0 {
		tr.pendingWaiters = append(tr.pendingWaiters, waiters...)
	}
	tr.mu.Unlock()
	tr.m.startMu.RUnlock()
}

func (tr *taskRuntime) callOnRunStart(ctx context.Context, info RunStartInfo) {
	if tr.onRunStartGlobal != nil {
		callHookNoPanic(ctx, tr.onRunStartGlobal, tr.name, tr.tags, info)
	}
	if tr.onRunStartLocal != nil {
		callHookNoPanic(ctx, tr.onRunStartLocal, tr.name, tr.tags, info)
	}
}

func (tr *taskRuntime) callOnRunFinish(ctx context.Context, info RunFinishInfo) {
	if tr.onRunFinishGlobal != nil {
		callHookNoPanic(ctx, tr.onRunFinishGlobal, tr.name, tr.tags, info)
	}
	if tr.onRunFinishLocal != nil {
		callHookNoPanic(ctx, tr.onRunFinishLocal, tr.name, tr.tags, info)
	}
}

func (tr *taskRuntime) removePendingWaiter(w runWaiter) bool {
	if w == nil {
		return false
	}
	tr.mu.Lock()
	defer tr.mu.Unlock()
	for i := range tr.pendingWaiters {
		if tr.pendingWaiters[i] != w {
			continue
		}
		// delete without preserving order
		last := len(tr.pendingWaiters) - 1
		tr.pendingWaiters[i] = tr.pendingWaiters[last]
		tr.pendingWaiters[last] = nil
		tr.pendingWaiters = tr.pendingWaiters[:last]
		return true
	}
	return false
}

func (tr *taskRuntime) runScheduler(ctx context.Context) {
	// baseTime is set in onManagerStart; for tasks added after start, it's set to Add time.
	tr.mu.Lock()
	base := tr.baseTime
	interval := tr.interval
	mode := tr.everyMode
	startImmediately := tr.startImmediately
	tr.mu.Unlock()

	if base.IsZero() {
		base = time.Now()
	}

	if mode == EveryFixedDelay {
		tr.runSchedulerFixedDelay(ctx, base, interval, startImmediately)
		return
	}
	tr.runSchedulerFixedRate(ctx, base, interval, startImmediately)
}

func (tr *taskRuntime) runSchedulerFixedDelay(ctx context.Context, base time.Time, interval time.Duration, startImmediately bool) {
	next := base
	if !startImmediately {
		next = base.Add(interval)
	}

	timer := time.NewTimer(time.Until(next))
	defer timer.Stop()

	for {
		tr.setNextRun(next)
		done := tr.doneChSnapshot()
		select {
		case <-ctx.Done():
			tr.setStopping()
			return
		case <-timer.C:
			// Snapshot the completion channel before scheduling the run opportunity,
			// so we never miss a fast completion (close+replace) and deadlock waiting.
			doneAfterSchedule := tr.doneChSnapshot()

			// Schedule a run opportunity.
			_, _ = tr.requestRun(RunKindSchedule, next, nil)
			// Next run is based on the latest completion time (any run).
			// Wait for a completion or ctx. The completion signal is never dropped.
			select {
			case <-ctx.Done():
				tr.setStopping()
				return
			case <-doneAfterSchedule:
				next = tr.nextAfterLastFinished(interval)
				resetTimer(timer, next)
			}
		case <-done:
			// A run finished earlier than our next timer; recompute next from that completion.
			next = tr.nextAfterLastFinished(interval)
			resetTimer(timer, next)
		}
	}
}

func (tr *taskRuntime) runSchedulerFixedRate(ctx context.Context, base time.Time, interval time.Duration, startImmediately bool) {
	// If requested, run immediately once (scheduled at base).
	if startImmediately {
		_, _ = tr.requestRun(RunKindSchedule, base, nil)
	}

	// Next tick is always strictly after now, aligned to base+K*interval with K>=1.
	next := nextTickAfter(base, interval, time.Now())

	timer := time.NewTimer(time.Until(next))
	defer timer.Stop()

	for {
		tr.setNextRun(next)
		select {
		case <-ctx.Done():
			tr.setStopping()
			return
		case <-timer.C:
			_, _ = tr.requestRun(RunKindSchedule, next, nil)
			// Never catch up: compute next future tick from now.
			next = nextTickAfter(base, interval, time.Now())
			resetTimer(timer, next)
		}
	}
}

func nextTickAfter(base time.Time, interval time.Duration, now time.Time) time.Time {
	if interval <= 0 {
		return now
	}
	if now.Before(base) {
		// This shouldn't happen, but be safe.
		return base.Add(interval)
	}
	d := now.Sub(base)
	k := int64(d/interval) + 1 // strictly after now
	return base.Add(time.Duration(k) * interval)
}

func (tr *taskRuntime) setNextRun(next time.Time) {
	tr.mu.Lock()
	tr.nextRun = next
	tr.mu.Unlock()
}

func (tr *taskRuntime) doneChSnapshot() <-chan struct{} {
	tr.mu.Lock()
	ch := tr.doneCh
	tr.mu.Unlock()
	return ch
}

func (tr *taskRuntime) rotateDoneChLocked() {
	// tr.mu must be held.
	// Closing a channel is a broadcast; any waiters will be released.
	close(tr.doneCh)
	tr.doneCh = make(chan struct{})
}

func (tr *taskRuntime) nextAfterLastFinished(interval time.Duration) time.Time {
	tr.mu.Lock()
	lastFinished := tr.lastFinished
	tr.mu.Unlock()
	if lastFinished.IsZero() {
		lastFinished = time.Now()
	}
	return lastFinished.Add(interval)
}

func (tr *taskRuntime) setStopping() {
	tr.mu.Lock()
	tr.state = StateStopping
	tr.mu.Unlock()
}

func (tr *taskRuntime) markStopped() {
	waiters := tr.drainPendingWaitersLocked(StateStopped)
	for _, w := range waiters {
		if w == nil {
			continue
		}
		select {
		case w <- runResult{err: ErrClosed}:
		default:
		}
	}
}

func (tr *taskRuntime) onManagerStopping() {
	waiters := tr.drainPendingWaitersLocked(StateStopping)
	for _, w := range waiters {
		if w == nil {
			continue
		}
		select {
		case w <- runResult{err: ErrClosed}:
		default:
		}
	}
}

func (tr *taskRuntime) drainPendingWaitersLocked(state State) []runWaiter {
	tr.mu.Lock()
	tr.state = state
	tr.nextRun = time.Time{}
	tr.pending = false
	waiters := tr.pendingWaiters
	tr.pendingWaiters = nil
	tr.mu.Unlock()
	return waiters
}

func resetTimer(t *time.Timer, at time.Time) {
	d := time.Until(at)
	if d < 0 {
		d = 0
	}
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}

func callErrorHandlerNoPanic(ctx context.Context, h safego.ErrorHandler, name string, tags []safego.Tag, info safego.ErrorInfo) {
	defer func() {
		if p := recover(); p != nil {
			reportPanicToStderr(name, tags, fmt.Sprintf("task: error handler panicked: %v", p), debug.Stack())
		}
	}()
	h(ctx, info)
}

func callPanicHandlerNoPanic(ctx context.Context, h safego.PanicHandler, name string, tags []safego.Tag, info safego.PanicInfo) {
	defer func() {
		if p := recover(); p != nil {
			reportPanicToStderr(name, tags, fmt.Sprintf("task: panic handler panicked: %v", p), debug.Stack())
		}
	}()
	h(ctx, info)
}

func callHookNoPanic[T any](ctx context.Context, h func(T), name string, tags []safego.Tag, info T) {
	defer func() {
		if p := recover(); p != nil {
			reportPanicToStderr(name, tags, fmt.Sprintf("task: hook panicked: %v", p), debug.Stack())
		}
	}()
	h(info)
}

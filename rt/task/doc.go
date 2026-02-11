// Package task provides small, standard-library flavored background task primitives.
//
// # Design highlights
//
//   - Manager: holds tasks, starts schedulers, and coordinates graceful shutdown.
//   - Trigger task: runs on demand via Handle.Trigger/TryTrigger/TriggerAndWait.
//   - Every task: runs periodically, with either fixed-delay or fixed-rate scheduling.
//   - Overlap policy: Skip or Merge (bounded; no unbounded queue).
//   - Panic/error reporting: uses safego-style handlers and tags; by default reports to stderr.
//
// # Lifecycle
//
// Manager must be started explicitly:
//
//	m := task.NewManager()
//	h, _ := m.Add(task.Every(10*time.Second, work), task.WithName("refresh"))
//	_ = m.Start(ctx)
//	defer m.Shutdown(context.Background())
//
// Start is not idempotent: calling Start more than once returns ErrAlreadyStarted.
//
// Shutdown is safe to call even if Start was never called; it marks all registered tasks as stopped
// for observability.
//
// Shutdown cancels the manager context and waits for all internal goroutines (schedulers + runs)
// to exit. During/after Shutdown:
//   - Add returns ErrClosed
//   - Trigger/TryTrigger/TriggerAndWait are no-ops (TryTrigger=false; TriggerAndWait=ErrClosed)
//
// Triggering before Start is also a no-op (TryTrigger=false; TriggerAndWait=ErrNotRunning).
//
// # Trigger vs Every
//
// Trigger tasks run when triggered:
//
//	h, _ := m.Add(task.Trigger(func(ctx context.Context) error {
//		return rebuildIndex(ctx)
//	}), task.WithName("rebuild-index"))
//
//	h.Trigger() // fire-and-forget
//
// By default, Trigger uses OverlapMerge (maxConcurrent defaults to 1).
//
// Every tasks run periodically:
//
//	_, _ = m.Add(task.Every(10*time.Second, refreshCache),
//		task.WithName("cache-refresh"),
//	)
//
// By default, Every uses OverlapSkip (maxConcurrent defaults to 1).
//
// By default, Every does NOT run immediately on Start (first run happens after one interval).
// Use WithStartImmediately(true) to run immediately.
//
// # Fixed-delay vs fixed-rate
//
// EveryFixedDelay (default): next run is scheduled after a run finishes, then waits interval.
//
// Note: fixed-delay uses the completion time of the latest run, regardless of whether the run was
// triggered manually (Handle.Trigger) or scheduled. In other words, manual triggers also "reset"
// the delay window: the next fixed-delay tick will be interval after the most recent completion.
// If a tick happens while a run is still in-flight and the overlap policy is OverlapSkip, that tick
// is dropped; the scheduler still waits for the next completion to compute the following tick.
//
// EveryFixedRate: schedules run opportunities aligned to a base time and interval, but it never
// "catches up" by emitting multiple missed ticks; it only schedules the next tick.
//
// Base time:
//   - If a task is added before Manager.Start, base time is the Start time.
//   - If a task is added after Manager.Start, base time is the Add time.
//
// # Overlap policies (Skip / Merge)
//
// Under contention (max concurrency reached), run opportunities are handled as:
//   - OverlapSkip: drop the opportunity (TriggerAndWait returns ErrSkipped)
//   - OverlapMerge: coalesce all overlapping opportunities into one pending run
//
// This keeps behavior bounded; task does not provide unbounded queues.
//
// # Hooks
//
// Manager and task options may provide OnRunStart/OnRunFinish hooks.
//
// Hooks are called synchronously on the task execution path. They must be fast and must not
// block (avoid network I/O and long computations). If you need asynchronous processing,
// start your own goroutine or send to a buffered channel from the hook.
//
// # TriggerAndWait
//
// TriggerAndWait is useful for admin/ops endpoints or startup "run once now":
//
//	if err := h.TriggerAndWait(ctx); err != nil {
//		// ErrNotRunning / ErrClosed / ErrSkipped / ErrPanicked / ctx.Err() / or the task's error
//	}
//
// # Observability
//
// Task status can be observed via Handle.Status() or Manager.Snapshot(), which is designed for
// consumption by an ops layer:
//
//	snap := m.Snapshot()
//	if st, ok := snap.Get("cache-refresh"); ok {
//		_ = st.LastError
//		_ = st.NextRun
//	}
//
// Note on Status.LastError:
// LastError is updated only when a run fails (or panics), and it is not cleared on success.
// Treat it as "last failure" rather than "last run error"; use the timestamps/counters to
// interpret recency and outcome.
//
// # Names and lookup
//
// Task names are optional. If a task is named (WithName), the name is:
//   - normalized by strings.TrimSpace
//   - validated against [A-Za-z0-9._-]
//   - unique within the Manager
//
// Named tasks can be looked up by name:
//
//	h, ok := m.Lookup("cache-refresh")
//	_ = h
package task

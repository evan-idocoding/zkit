package task

import (
	"context"
	"fmt"
	"time"

	"github.com/evan-idocoding/zkit/rt/safego"
)

// Func is the user-provided function executed by a task.
//
// Returning a non-nil error is reported via the configured error handler (unless filtered),
// and may also be observed via TriggerAndWait.
type Func func(context.Context) error

// Task is a task definition that can be registered to a Manager.
type Task interface {
	kind() taskKind
}

type taskKind int

const (
	taskKindTrigger taskKind = iota
	taskKindEvery
)

// Handle is a registered task handle.
//
// Triggering before Start is a no-op (TryTrigger returns false).
// Triggering during/after Shutdown is a no-op (TryTrigger returns false).
type Handle interface {
	// Name returns the configured name (may be empty).
	Name() string

	// Trigger requests a run opportunity. It is equivalent to calling TryTrigger and
	// ignoring the return value.
	Trigger()

	// TryTrigger requests a run opportunity and reports whether it was accepted.
	TryTrigger() bool

	// TriggerAndWait requests a run opportunity and waits for its completion.
	//
	// Errors:
	//   - ErrNotRunning: Manager not started yet.
	//   - ErrClosed: Manager is shutting down or already stopped.
	//   - ErrSkipped: OverlapSkip drops this trigger due to concurrency.
	//   - ctx.Err(): ctx canceled/timeout while waiting.
	//
	// If the run panicked, TriggerAndWait returns ErrPanicked.
	// If the run returned an error (and it was not filtered), that error is returned.
	//
	// If ctx is nil, it is treated as context.Background().
	TriggerAndWait(ctx context.Context) error

	// Status returns a snapshot of the task's current status.
	Status() Status
}

// State is the high-level lifecycle state of a task.
type State int

const (
	StateNotStarted State = iota
	StateIdle
	StateRunning
	StateStopping
	StateStopped
)

func (s State) String() string {
	switch s {
	case StateNotStarted:
		return "not-started"
	case StateIdle:
		return "idle"
	case StateRunning:
		return "running"
	case StateStopping:
		return "stopping"
	case StateStopped:
		return "stopped"
	default:
		return fmt.Sprintf("State(%d)", int(s))
	}
}

// OverlapPolicy controls how overlapping run opportunities are handled.
type OverlapPolicy int

const (
	// OverlapSkip drops a run opportunity if max concurrency is reached.
	OverlapSkip OverlapPolicy = iota
	// OverlapMerge merges all overlapping run opportunities into a single pending run.
	OverlapMerge
)

func (p OverlapPolicy) String() string {
	switch p {
	case OverlapSkip:
		return "skip"
	case OverlapMerge:
		return "merge"
	default:
		return fmt.Sprintf("OverlapPolicy(%d)", int(p))
	}
}

// EveryMode controls the scheduling semantics of an Every task.
type EveryMode int

const (
	// EveryFixedDelay schedules the next run after a run finishes, then waits interval.
	EveryFixedDelay EveryMode = iota
	// EveryFixedRate schedules run opportunities aligned to a base time and interval.
	// It never "catches up" by emitting multiple missed ticks; it only schedules the next tick.
	EveryFixedRate
)

func (m EveryMode) String() string {
	switch m {
	case EveryFixedDelay:
		return "fixed-delay"
	case EveryFixedRate:
		return "fixed-rate"
	default:
		return fmt.Sprintf("EveryMode(%d)", int(m))
	}
}

// Status is a task state snapshot.
type Status struct {
	Name  string
	Tags  []safego.Tag
	State State

	Running int
	// Pending is true when OverlapMerge has a pending run opportunity.
	// Pending does not imply the task is currently running; check State/Running for that.
	Pending bool

	RunCount     uint64
	FailCount    uint64
	SuccessCount uint64
	// CanceledCount counts context cancellation / deadline exceeded that is filtered by
	// reportContextCancel=false (i.e. not reported, and not treated as success or failure).
	CanceledCount uint64

	LastStarted  time.Time
	LastFinished time.Time
	LastSuccess  time.Time

	LastDuration time.Duration
	// LastError is the most recent run error for this task *when the run failed*.
	//
	// Important semantics:
	//   - LastError is updated only when a run fails (non-nil error) or panics ("panic").
	//   - LastError is NOT cleared on success.
	//   - Therefore, LastError represents "last failure" rather than "last run error".
	//
	// Use LastFinished/LastSuccess and the counters (FailCount/SuccessCount/CanceledCount)
	// to interpret recency and outcome.
	LastError string

	// NextRun is the next scheduled time (Every tasks only). Zero for Trigger tasks.
	NextRun time.Time
}

// Snapshot is a point-in-time view of all tasks in a Manager.
type Snapshot struct {
	Tasks []Status
}

// Get finds a task status by name.
func (s Snapshot) Get(name string) (Status, bool) {
	for _, st := range s.Tasks {
		if st.Name == name {
			return st, true
		}
	}
	return Status{}, false
}

// Runner is a small lifecycle interface implemented by Manager for app assembly.
type Runner interface {
	Start(context.Context) error
	Shutdown(context.Context) error
	Wait()
}

// RunKind indicates why a run started.
type RunKind int

const (
	RunKindTrigger RunKind = iota
	RunKindSchedule
)

func (k RunKind) String() string {
	switch k {
	case RunKindTrigger:
		return "trigger"
	case RunKindSchedule:
		return "schedule"
	default:
		return fmt.Sprintf("RunKind(%d)", int(k))
	}
}

// RunStartInfo is passed to OnRunStart hooks.
type RunStartInfo struct {
	Name string
	Tags []safego.Tag

	Kind        RunKind
	ScheduledAt time.Time // non-zero for schedule-based runs (best-effort).

	StartedAt time.Time
}

// RunFinishInfo is passed to OnRunFinish hooks.
type RunFinishInfo struct {
	Name string
	Tags []safego.Tag

	Kind        RunKind
	ScheduledAt time.Time

	StartedAt  time.Time
	FinishedAt time.Time
	Duration   time.Duration

	Err      string
	Panicked bool
}

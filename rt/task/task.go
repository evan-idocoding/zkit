package task

import "time"

type triggerTask struct {
	fn Func
}

func (t triggerTask) kind() taskKind { return taskKindTrigger }

// Trigger creates a trigger-based task.
func Trigger(fn Func) Task {
	return triggerTask{fn: fn}
}

type everyTask struct {
	interval time.Duration
	fn       Func
}

func (t everyTask) kind() taskKind { return taskKindEvery }

// Every creates a periodic task that runs at the given interval.
//
// Default scheduling mode is EveryFixedDelay.
// Default startImmediately is false (first run happens after one interval).
//
// The interval must be > 0; otherwise Manager.Add panics (configuration error).
func Every(interval time.Duration, fn Func) Task {
	return everyTask{interval: interval, fn: fn}
}

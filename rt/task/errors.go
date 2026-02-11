package task

import "errors"

var (
	// ErrAlreadyStarted is returned by Start when called more than once.
	ErrAlreadyStarted = errors.New("task: manager already started")
	// ErrClosed is returned when the manager is shutting down or already stopped.
	ErrClosed = errors.New("task: manager closed")
	// ErrNotRunning is returned when an operation requires a running manager.
	ErrNotRunning = errors.New("task: manager not running")
	// ErrSkipped indicates a run opportunity was dropped due to OverlapSkip.
	ErrSkipped = errors.New("task: trigger skipped")
	// ErrPanicked indicates a run panicked (panic is recovered and reported).
	ErrPanicked = errors.New("task: run panicked")

	// ErrInvalidName is returned by Add when a task name is invalid.
	//
	// Name rules:
	//   - name is optional (empty means unnamed)
	//   - non-empty name must match [A-Za-z0-9._-]
	//   - name is normalized by strings.TrimSpace before validation
	ErrInvalidName = errors.New("task: invalid name")

	// ErrDuplicateName is returned by Add when a non-empty task name is already registered.
	//
	// Names are unique within a manager (after normalization).
	ErrDuplicateName = errors.New("task: duplicate name")
)

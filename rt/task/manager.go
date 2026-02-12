package task

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type managerState int32

const (
	managerNotStarted managerState = iota
	managerRunning
	managerStopping
	managerStopped
)

// Manager coordinates tasks and their lifecycles.
//
// It is safe for concurrent use.
//
// The zero value is ready to use with default configuration.
// To apply ManagerOption (hooks/handlers), use NewManager.
type Manager struct {
	state atomic.Int32 // managerState

	cfg managerConfig

	mu    sync.Mutex
	tasks []*taskRuntime
	names map[string]Handle // normalized name -> handle (non-empty only)

	startMu   sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	startTime time.Time

	wg sync.WaitGroup // schedulers + runs
}

// NewManager creates a new Manager.
func NewManager(opts ...ManagerOption) *Manager {
	var cfg managerConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &Manager{
		cfg:   cfg,
		names: make(map[string]Handle),
	}
}

// Add registers a task and returns its handle.
//
// It can be called before or after Start. If called during/after Shutdown, it returns ErrClosed.
func (m *Manager) Add(t Task, opts ...Option) (Handle, error) {
	if t == nil {
		panic("task: Add called with nil Task")
	}
	m.startMu.RLock()
	st := managerState(m.state.Load())
	if st >= managerStopping {
		m.startMu.RUnlock()
		return nil, ErrClosed
	}
	// Capture ctx for immediate activation if already running.
	ctx := m.ctx
	var base time.Time
	if st == managerRunning {
		// Capture the base time under the same lock used for state transitions,
		// to align the "added after Start" baseTime as closely as possible to Add.
		base = time.Now()
	}
	m.startMu.RUnlock()

	c := taskConfigFrom(m, t, opts)
	c.name = normalizeName(c.name)
	if err := validateName(c.name); err != nil {
		return nil, fmt.Errorf("%w: %q: %v", ErrInvalidName, c.name, err)
	}

	tr := newTaskRuntimeFromConfig(m, t, c)

	m.mu.Lock()
	if c.name != "" {
		if m.names == nil {
			m.names = make(map[string]Handle)
		}
		if _, exists := m.names[c.name]; exists {
			m.mu.Unlock()
			return nil, fmt.Errorf("%w: %q", ErrDuplicateName, c.name)
		}
	}
	m.tasks = append(m.tasks, tr)
	if c.name != "" {
		m.names[c.name] = tr
	}
	m.mu.Unlock()

	// If manager is already running, activate the task immediately.
	if st == managerRunning {
		tr.onManagerStart(ctx, base)
	}
	return tr, nil
}

// MustAdd is like Add but panics on error.
//
// It is intended for initialization-time wiring where an error indicates a programming/configuration
// mistake (for example, invalid name or duplicate name).
func (m *Manager) MustAdd(t Task, opts ...Option) Handle {
	h, err := m.Add(t, opts...)
	if err != nil {
		panic(err)
	}
	return h
}

// Start starts all tasks and their schedulers.
//
// Start is not idempotent: calling it more than once returns ErrAlreadyStarted.
// If ctx is nil, it is treated as context.Background().
func (m *Manager) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.startMu.Lock()
	if managerState(m.state.Load()) != managerNotStarted {
		m.startMu.Unlock()
		return ErrAlreadyStarted
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.startTime = time.Now()
	m.state.Store(int32(managerRunning))
	startCtx := m.ctx
	startTime := m.startTime
	m.startMu.Unlock()

	m.mu.Lock()
	tasks := append([]*taskRuntime(nil), m.tasks...)
	m.mu.Unlock()

	for _, tr := range tasks {
		tr.onManagerStart(startCtx, startTime)
	}
	return nil
}

// Shutdown stops scheduling new work, cancels task contexts, and waits for running tasks to finish.
//
// Shutdown is safe to call multiple times. It is also safe to call without a prior Start; in that case,
// it marks tasks as stopped for observability and returns nil.
//
// If ctx is nil, it is treated as context.Background().
func (m *Manager) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	m.startMu.Lock()
	st := managerState(m.state.Load())
	switch st {
	case managerNotStarted:
		m.state.Store(int32(managerStopped))
		m.startMu.Unlock()
		// Mark tasks as stopped for observability (even if Start was never called).
		m.mu.Lock()
		tasks := append([]*taskRuntime(nil), m.tasks...)
		m.mu.Unlock()
		for _, tr := range tasks {
			tr.markStopped()
		}
		return nil
	case managerRunning:
		m.state.Store(int32(managerStopping))
		cancel := m.cancel
		m.startMu.Unlock()
		// Release any pending TriggerAndWait waiters immediately.
		m.mu.Lock()
		tasks := append([]*taskRuntime(nil), m.tasks...)
		m.mu.Unlock()
		for _, tr := range tasks {
			tr.onManagerStopping()
		}
		if cancel != nil {
			cancel()
		}
	case managerStopping:
		// Already stopping (e.g. a previous Shutdown timed out). Wait again with the new ctx.
		m.startMu.Unlock()
	case managerStopped:
		m.startMu.Unlock()
		return nil
	default:
		m.startMu.Unlock()
		return nil
	}

	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.state.Store(int32(managerStopped))
		// Mark tasks as stopped for observability.
		m.mu.Lock()
		tasks := append([]*taskRuntime(nil), m.tasks...)
		m.mu.Unlock()
		for _, tr := range tasks {
			tr.markStopped()
		}
		return nil
	case <-ctx.Done():
		// Keep state as stopping; caller can call Wait or Shutdown again.
		return ctx.Err()
	}
}

// Wait waits until all internal goroutines (schedulers + runs) have exited.
func (m *Manager) Wait() {
	m.wg.Wait()
}

// Snapshot returns a point-in-time view of all tasks.
func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	tasks := append([]*taskRuntime(nil), m.tasks...)
	m.mu.Unlock()

	out := make([]Status, 0, len(tasks))
	for _, tr := range tasks {
		out = append(out, tr.Status())
	}
	return Snapshot{Tasks: out}
}

// Lookup finds a task handle by name.
//
// Name is normalized by strings.TrimSpace. Empty names are not indexed and always return (nil, false).
// Lookup is safe for concurrent use.
func (m *Manager) Lookup(name string) (Handle, bool) {
	if m == nil {
		return nil, false
	}
	name = normalizeName(name)
	if name == "" {
		return nil, false
	}
	m.mu.Lock()
	h, ok := m.names[name]
	m.mu.Unlock()
	return h, ok
}

func (m *Manager) managerState() managerState {
	return managerState(m.state.Load())
}

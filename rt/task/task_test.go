package task

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/evan-idocoding/zkit/rt/safego"
)

func TestStart_AlreadyStarted(t *testing.T) {
	t.Parallel()

	m := NewManager()
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v, want nil", err)
	}
	if err := m.Start(context.Background()); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("Start err=%v, want ErrAlreadyStarted", err)
	}
	_ = m.Shutdown(context.Background())
}

func TestAdd_Name_NormalizeValidateAndLookup(t *testing.T) {
	t.Parallel()

	m := NewManager()
	h, err := m.Add(Trigger(func(context.Context) error { return nil }), WithName("  x.y_z-9  "))
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}
	if got := h.Name(); got != "x.y_z-9" {
		t.Fatalf("Name=%q, want %q", got, "x.y_z-9")
	}

	if got, ok := m.Lookup("x.y_z-9"); !ok || got != h {
		t.Fatalf("Lookup returned ok=%v, got=%v; want ok=true, got==h", ok, got)
	}
	if got, ok := m.Lookup("  x.y_z-9  "); !ok || got != h {
		t.Fatalf("Lookup(trimmed) returned ok=%v, got=%v; want ok=true, got==h", ok, got)
	}
}

func TestAdd_Name_Invalid_ReturnsErrInvalidName(t *testing.T) {
	t.Parallel()

	m := NewManager()
	_, err := m.Add(Trigger(func(context.Context) error { return nil }), WithName("a/b"))
	if !errors.Is(err, ErrInvalidName) {
		t.Fatalf("Add err=%v, want ErrInvalidName", err)
	}
	_, err = m.Add(Trigger(func(context.Context) error { return nil }), WithName("a b"))
	if !errors.Is(err, ErrInvalidName) {
		t.Fatalf("Add err=%v, want ErrInvalidName", err)
	}
}

func TestAdd_Name_Duplicate_ReturnsErrDuplicateName(t *testing.T) {
	t.Parallel()

	m := NewManager()
	_, err := m.Add(Trigger(func(context.Context) error { return nil }), WithName("x"))
	if err != nil {
		t.Fatalf("first Add err=%v", err)
	}
	_, err = m.Add(Trigger(func(context.Context) error { return nil }), WithName("  x  "))
	if !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("second Add err=%v, want ErrDuplicateName", err)
	}
}

func TestLookup_EmptyOrUnnamed_ReturnsFalse(t *testing.T) {
	t.Parallel()

	m := NewManager()
	if got, ok := m.Lookup(""); ok || got != nil {
		t.Fatalf("Lookup(\"\") ok=%v got=%v, want ok=false got=nil", ok, got)
	}
	if got, ok := m.Lookup("   "); ok || got != nil {
		t.Fatalf("Lookup(blank) ok=%v got=%v, want ok=false got=nil", ok, got)
	}

	// Unnamed tasks are not indexed.
	_, err := m.Add(Trigger(func(context.Context) error { return nil }))
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}
	if got, ok := m.Lookup("unnamed"); ok || got != nil {
		t.Fatalf("Lookup(unnamed) ok=%v got=%v, want ok=false got=nil", ok, got)
	}
}

func TestLookup_AfterShutdown_StillWorks(t *testing.T) {
	t.Parallel()

	m := NewManager()
	h, err := m.Add(Trigger(func(context.Context) error { return nil }), WithName("x"))
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}
	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown err=%v", err)
	}

	if got, ok := m.Lookup("x"); !ok || got != h {
		t.Fatalf("Lookup after Shutdown ok=%v got=%v, want ok=true got==h", ok, got)
	}
}

func TestTrigger_BeforeStart_IsNoOp(t *testing.T) {
	t.Parallel()

	m := NewManager()
	var runs atomic.Int64
	h, err := m.Add(Trigger(func(context.Context) error {
		runs.Add(1)
		return nil
	}), WithName("x"))
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}

	h.Trigger()
	time.Sleep(20 * time.Millisecond)
	if got := runs.Load(); got != 0 {
		t.Fatalf("runs=%d, want 0", got)
	}
	if ok := h.TryTrigger(); ok {
		t.Fatalf("TryTrigger=%v, want false", ok)
	}
	if err := h.TriggerAndWait(context.Background()); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("TriggerAndWait err=%v, want ErrNotRunning", err)
	}
}

func TestShutdown_ThenTriggerIsNoOp(t *testing.T) {
	t.Parallel()

	m := NewManager()
	var runs atomic.Int64
	h, err := m.Add(Trigger(func(context.Context) error {
		runs.Add(1)
		return nil
	}))
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}
	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown err=%v", err)
	}

	if ok := h.TryTrigger(); ok {
		t.Fatalf("TryTrigger=%v, want false", ok)
	}
	if err := h.TriggerAndWait(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("TriggerAndWait err=%v, want ErrClosed", err)
	}
	if got := runs.Load(); got != 0 {
		t.Fatalf("runs=%d, want 0", got)
	}

	st := h.Status()
	if st.State != StateStopped {
		t.Fatalf("state=%v, want StateStopped", st.State)
	}
}

func TestAdd_AfterShutdown_ReturnsErrClosed(t *testing.T) {
	t.Parallel()

	m := NewManager()
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}
	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown err=%v", err)
	}
	_, err := m.Add(Trigger(func(context.Context) error { return nil }))
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("Add err=%v, want ErrClosed", err)
	}
}

func TestShutdown_WithoutStart_MarksTasksStopped(t *testing.T) {
	t.Parallel()

	m := NewManager()
	h, err := m.Add(Trigger(func(context.Context) error { return nil }), WithName("x"))
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}
	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown err=%v", err)
	}
	st := h.Status()
	if st.State != StateStopped {
		t.Fatalf("state=%v, want StateStopped", st.State)
	}
	if ok := h.TryTrigger(); ok {
		t.Fatalf("TryTrigger=%v, want false after Shutdown", ok)
	}
}

func TestAdd_DuringShutdown_ReturnsErrClosed(t *testing.T) {
	t.Parallel()

	m := NewManager()

	block := make(chan struct{})
	started := make(chan struct{}, 1)
	h, err := m.Add(Trigger(func(context.Context) error {
		select {
		case started <- struct{}{}:
		default:
		}
		<-block
		return nil
	}), WithOverlapPolicy(OverlapMerge))
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}
	_ = h

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}

	// Ensure at least one run is in-flight so Shutdown stays in stopping state.
	go func() {
		_ = h.TriggerAndWait(context.Background())
	}()
	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("run did not start")
	}

	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- m.Shutdown(context.Background())
	}()

	// Wait until manager enters stopping.
	deadline := time.Now().Add(200 * time.Millisecond)
	for m.managerState() != managerStopping {
		if time.Now().After(deadline) {
			t.Fatalf("manager did not enter stopping state")
		}
		time.Sleep(1 * time.Millisecond)
	}

	_, err = m.Add(Trigger(func(context.Context) error { return nil }))
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("Add err=%v, want ErrClosed", err)
	}

	close(block)
	if err := <-shutdownDone; err != nil {
		t.Fatalf("Shutdown err=%v", err)
	}
}

func TestShutdown_ReleasesPendingTriggerAndWait(t *testing.T) {
	t.Parallel()

	m := NewManager()

	gate := make(chan struct{})
	started := make(chan struct{}, 1)

	h, err := m.Add(Trigger(func(context.Context) error {
		select {
		case started <- struct{}{}:
		default:
		}
		<-gate
		return nil
	}), WithMaxConcurrent(1), WithOverlapPolicy(OverlapMerge))
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}

	// Start first run and block it.
	go func() { _ = h.TriggerAndWait(context.Background()) }()
	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("first run did not start")
	}

	// Second TriggerAndWait will be pending (merge).
	errCh := make(chan error, 1)
	go func() {
		errCh <- h.TriggerAndWait(context.Background())
	}()

	// Ensure it has time to enqueue pending waiter.
	time.Sleep(20 * time.Millisecond)

	// Shutdown should release the pending waiter quickly with ErrClosed.
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- m.Shutdown(context.Background()) }()

	select {
	case err := <-errCh:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("pending TriggerAndWait err=%v, want ErrClosed", err)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatalf("pending TriggerAndWait did not return; likely leaked waiter")
	}

	close(gate)
	_ = <-shutdownDone
}

func TestShutdown_TimedOutCanBeCalledAgainToWait(t *testing.T) {
	t.Parallel()

	m := NewManager()
	block := make(chan struct{})
	started := make(chan struct{}, 1)

	h, err := m.Add(Trigger(func(context.Context) error {
		select {
		case started <- struct{}{}:
		default:
		}
		<-block
		return nil
	}))
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}

	go func() { _ = h.TriggerAndWait(context.Background()) }()
	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("run did not start")
	}

	ctx1, cancel1 := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel1()
	if err := m.Shutdown(ctx1); err == nil {
		t.Fatalf("first Shutdown err=nil, want timeout")
	}

	// Unblock the running task, then Shutdown again should wait and complete.
	close(block)
	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown err=%v, want nil", err)
	}
	if st := h.Status().State; st != StateStopped {
		t.Fatalf("state=%v, want StateStopped", st)
	}
}

func TestOverlapSkip_SecondTriggerSkipped(t *testing.T) {
	t.Parallel()

	m := NewManager()

	block := make(chan struct{})
	started := make(chan struct{}, 1)

	h, err := m.Add(Trigger(func(context.Context) error {
		select {
		case started <- struct{}{}:
		default:
		}
		<-block
		return nil
	}), WithMaxConcurrent(1), WithOverlapPolicy(OverlapSkip))
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}
	defer m.Shutdown(context.Background())

	// Start first run.
	go func() {
		_ = h.TriggerAndWait(context.Background())
	}()
	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("first run did not start")
	}

	// Second should be skipped.
	err = h.TriggerAndWait(context.Background())
	if !errors.Is(err, ErrSkipped) {
		t.Fatalf("second TriggerAndWait err=%v, want ErrSkipped", err)
	}

	close(block)
}

func TestTriggerAndWait_CtxCancel_RemovesPendingWaiter(t *testing.T) {
	t.Parallel()

	m := NewManager()

	block := make(chan struct{})
	started := make(chan struct{}, 1)

	h, err := m.Add(Trigger(func(context.Context) error {
		select {
		case started <- struct{}{}:
		default:
		}
		<-block
		return nil
	}), WithMaxConcurrent(1), WithOverlapPolicy(OverlapMerge))
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}
	tr := h.(*taskRuntime)

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}
	defer m.Shutdown(context.Background())

	// Start first run and block it.
	go func() { _ = h.TriggerAndWait(context.Background()) }()
	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("first run did not start")
	}

	// Second TriggerAndWait will enqueue a pending waiter (merge), then time out.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = h.TriggerAndWait(ctx)
	if err == nil {
		t.Fatalf("TriggerAndWait err=nil, want ctx.Err()")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("TriggerAndWait err=%v, want ctx cancel/timeout", err)
	}

	// The waiter should be removed from pendingWaiters to avoid growth.
	tr.mu.Lock()
	n := len(tr.pendingWaiters)
	tr.mu.Unlock()
	if n != 0 {
		t.Fatalf("pendingWaiters=%d, want 0 after ctx cancel", n)
	}

	close(block)
}

func TestOverlapMerge_TriggerAndWaitWaitsForMergedRun(t *testing.T) {
	t.Parallel()

	m := NewManager()

	var runs atomic.Int64
	// Gate run progression so we can reliably trigger overlap.
	gate1 := make(chan struct{})
	gate2 := make(chan struct{})
	started := make(chan int64, 2)

	h, err := m.Add(Trigger(func(context.Context) error {
		n := runs.Add(1)
		started <- n
		if n == 1 {
			<-gate1
		} else if n == 2 {
			<-gate2
		}
		return nil
	}), WithMaxConcurrent(1), WithOverlapPolicy(OverlapMerge))
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}
	defer m.Shutdown(context.Background())

	errCh1 := make(chan error, 1)
	errCh2 := make(chan error, 1)

	go func() { errCh1 <- h.TriggerAndWait(context.Background()) }()
	// Wait until first run starts.
	select {
	case n := <-started:
		if n != 1 {
			t.Fatalf("first started=%d, want 1", n)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("first run did not start")
	}

	// Trigger during first run; should merge and then wait for the second run to complete.
	go func() { errCh2 <- h.TriggerAndWait(context.Background()) }()

	// Allow first run to finish.
	close(gate1)
	if err := <-errCh1; err != nil {
		t.Fatalf("first TriggerAndWait err=%v", err)
	}

	// Second run should start after first finishes.
	select {
	case n := <-started:
		if n != 2 {
			t.Fatalf("second started=%d, want 2", n)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("second run did not start")
	}
	close(gate2)
	if err := <-errCh2; err != nil {
		t.Fatalf("second TriggerAndWait err=%v", err)
	}

	if got := runs.Load(); got != 2 {
		t.Fatalf("runs=%d, want 2", got)
	}
}

func TestNoNewRunsStartAfterShutdownBegins(t *testing.T) {
	t.Parallel()

	var starts atomic.Int64
	m := NewManager(WithManagerOnRunStart(func(RunStartInfo) { starts.Add(1) }))

	block := make(chan struct{})
	firstStarted := make(chan struct{}, 1)

	h, err := m.Add(Trigger(func(context.Context) error {
		select {
		case firstStarted <- struct{}{}:
		default:
		}
		<-block
		return nil
	}), WithMaxConcurrent(1), WithOverlapPolicy(OverlapMerge))
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}

	// Start first run and block it.
	go func() { _ = h.TriggerAndWait(context.Background()) }()
	select {
	case <-firstStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("first run did not start")
	}

	// Begin shutdown (will block waiting for the running run).
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- m.Shutdown(context.Background()) }()

	// Wait until manager enters stopping.
	deadline := time.Now().Add(200 * time.Millisecond)
	for m.managerState() != managerStopping {
		if time.Now().After(deadline) {
			t.Fatalf("manager did not enter stopping state")
		}
		time.Sleep(1 * time.Millisecond)
	}

	// Try to trigger a new run after shutdown begins: must be rejected.
	if ok := h.TryTrigger(); ok {
		t.Fatalf("TryTrigger=%v, want false after shutdown begins", ok)
	}

	// Ensure we did not start a second run.
	if got := starts.Load(); got != 1 {
		t.Fatalf("run starts=%d, want 1 (no new runs after shutdown begins)", got)
	}

	close(block)
	if err := <-shutdownDone; err != nil {
		t.Fatalf("Shutdown err=%v", err)
	}
}

func TestNoNewScheduleRunsStartAfterShutdownBegins(t *testing.T) {
	t.Parallel()

	var starts atomic.Int64
	m := NewManager(WithManagerOnRunStart(func(info RunStartInfo) {
		if info.Kind == RunKindSchedule {
			starts.Add(1)
		}
	}))

	block := make(chan struct{})
	firstStarted := make(chan struct{}, 1)

	_, err := m.Add(Every(15*time.Millisecond, func(context.Context) error {
		select {
		case firstStarted <- struct{}{}:
		default:
		}
		<-block
		return nil
	}),
		WithEveryMode(EveryFixedRate),
		WithStartImmediately(true),
		WithMaxConcurrent(1),
		WithOverlapPolicy(OverlapSkip),
	)
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}

	// First scheduled run should start quickly and then block.
	select {
	case <-firstStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("first scheduled run did not start")
	}

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- m.Shutdown(context.Background()) }()

	// Wait until manager enters stopping.
	deadline := time.Now().Add(200 * time.Millisecond)
	for m.managerState() != managerStopping {
		if time.Now().After(deadline) {
			t.Fatalf("manager did not enter stopping state")
		}
		time.Sleep(1 * time.Millisecond)
	}

	// Wait longer than the interval; if scheduler could start new runs after stopping,
	// we would see starts increase.
	time.Sleep(80 * time.Millisecond)

	if got := starts.Load(); got != 1 {
		t.Fatalf("scheduled run starts=%d, want 1 (no new schedule runs after shutdown begins)", got)
	}

	close(block)
	if err := <-shutdownDone; err != nil {
		t.Fatalf("Shutdown err=%v", err)
	}
}

func TestEvery_DefaultFirstRunAfterInterval(t *testing.T) {
	t.Parallel()

	m := NewManager()
	var runs atomic.Int64
	interval := 30 * time.Millisecond

	_, err := m.Add(Every(interval, func(context.Context) error {
		runs.Add(1)
		return nil
	}))
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}
	defer m.Shutdown(context.Background())

	time.Sleep(interval / 2)
	if got := runs.Load(); got != 0 {
		t.Fatalf("runs=%d, want 0 before first interval", got)
	}
	time.Sleep(interval + 40*time.Millisecond)
	if got := runs.Load(); got == 0 {
		t.Fatalf("runs=%d, want >=1 after interval", got)
	}
}

func TestEvery_StartImmediately_FixedRate(t *testing.T) {
	t.Parallel()

	m := NewManager()
	started := make(chan struct{}, 1)

	_, err := m.Add(Every(200*time.Millisecond, func(context.Context) error {
		select {
		case started <- struct{}{}:
		default:
		}
		return nil
	}), WithEveryMode(EveryFixedRate), WithStartImmediately(true))
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}
	defer m.Shutdown(context.Background())

	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected immediate run for fixed-rate+startImmediately")
	}
}

func TestEvery_FixedRate_NoCatchUpWithSlowRun(t *testing.T) {
	t.Parallel()

	m := NewManager()
	var runs atomic.Int64

	interval := 15 * time.Millisecond
	_, err := m.Add(Every(interval, func(context.Context) error {
		runs.Add(1)
		time.Sleep(50 * time.Millisecond)
		return nil
	}), WithEveryMode(EveryFixedRate))
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}
	defer m.Shutdown(context.Background())

	// Give enough time for at most 2 starts if we do not catch up.
	time.Sleep(120 * time.Millisecond)
	if got := runs.Load(); got > 2 {
		t.Fatalf("runs=%d, want <=2 (no catch-up)", got)
	}
}

func TestEvery_FixedDelay_SkipDoesNotDeadlockScheduler(t *testing.T) {
	t.Parallel()

	m := NewManager()

	interval := 20 * time.Millisecond

	gate := make(chan struct{})
	started := make(chan int, 4)

	var runN atomic.Int64
	_, err := m.Add(Every(interval, func(context.Context) error {
		n := int(runN.Add(1))
		select {
		case started <- n:
		default:
		}
		if n == 1 {
			<-gate
		}
		return nil
	}),
		WithEveryMode(EveryFixedDelay),
		WithStartImmediately(true),
		WithMaxConcurrent(1),
		WithOverlapPolicy(OverlapSkip),
	)
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}
	defer m.Shutdown(context.Background())

	// First run should start quickly and then block.
	select {
	case n := <-started:
		if n != 1 {
			t.Fatalf("started=%d, want 1", n)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("first run did not start")
	}

	// Let some timer ticks happen while run 1 is in-flight.
	time.Sleep(3 * interval)

	// Unblock run 1; scheduler should not be stuck and should eventually start run 2.
	close(gate)

	select {
	case n := <-started:
		if n != 2 {
			t.Fatalf("started=%d, want 2", n)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatalf("second run did not start; fixed-delay scheduler likely deadlocked")
	}
}

func TestHandlersAndHooks_PanicContainedAndReportedToHandler(t *testing.T) {
	t.Parallel()

	mgrPanics := atomic.Int64{}
	taskPanics := atomic.Int64{}
	hookStart := atomic.Int64{}
	hookFinish := atomic.Int64{}

	m := NewManager(
		WithManagerPanicHandler(func(context.Context, safego.PanicInfo) { mgrPanics.Add(1) }),
		WithManagerOnRunStart(func(RunStartInfo) { hookStart.Add(1); panic("hook boom") }),
		WithManagerOnRunFinish(func(RunFinishInfo) { hookFinish.Add(1); panic("hook boom") }),
	)

	h, err := m.Add(Trigger(func(context.Context) error { panic("boom") }),
		WithPanicHandler(func(context.Context, safego.PanicInfo) { taskPanics.Add(1) }),
	)
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}
	defer m.Shutdown(context.Background())

	if err := h.TriggerAndWait(context.Background()); !errors.Is(err, ErrPanicked) {
		t.Fatalf("TriggerAndWait err=%v, want ErrPanicked", err)
	}
	if got := mgrPanics.Load(); got != 0 {
		t.Fatalf("manager panic handler called=%d, want 0 (task handler overrides)", got)
	}
	if got := taskPanics.Load(); got != 1 {
		t.Fatalf("task panic handler called=%d, want 1", got)
	}
	if hookStart.Load() == 0 || hookFinish.Load() == 0 {
		t.Fatalf("hooks not called: start=%d finish=%d", hookStart.Load(), hookFinish.Load())
	}
}

func TestErrorHandler_ContextCancelFilteredByDefault(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	m := NewManager(WithManagerErrorHandler(func(context.Context, safego.ErrorInfo) { calls.Add(1) }))
	h, err := m.Add(Trigger(func(context.Context) error { return context.Canceled }))
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}
	defer m.Shutdown(context.Background())

	if err := h.TriggerAndWait(context.Background()); err != nil {
		t.Fatalf("TriggerAndWait err=%v, want nil (filtered)", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("error handler calls=%d, want 0", got)
	}
	st := h.Status()
	if !st.LastSuccess.IsZero() {
		t.Fatalf("LastSuccess=%v, want zero (filtered cancel is not success)", st.LastSuccess)
	}
	if st.FailCount != 0 {
		t.Fatalf("FailCount=%d, want 0 (filtered cancel is not failure)", st.FailCount)
	}
	if st.SuccessCount != 0 {
		t.Fatalf("SuccessCount=%d, want 0 (filtered cancel is not success)", st.SuccessCount)
	}
	if st.CanceledCount != 1 {
		t.Fatalf("CanceledCount=%d, want 1 (filtered cancel counted separately)", st.CanceledCount)
	}
}

func TestErrorHandler_ReportContextCancelWhenEnabled(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	m := NewManager(WithManagerErrorHandler(func(context.Context, safego.ErrorInfo) { calls.Add(1) }))
	h, err := m.Add(Trigger(func(context.Context) error { return context.DeadlineExceeded }),
		WithReportContextCancel(true),
	)
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}
	defer m.Shutdown(context.Background())

	if err := h.TriggerAndWait(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("TriggerAndWait err=%v, want context.DeadlineExceeded", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("error handler calls=%d, want 1", got)
	}
}

func TestStatus_SuccessAndFailAndCanceledCounts(t *testing.T) {
	t.Parallel()

	m := NewManager()
	hOK, err := m.Add(Trigger(func(context.Context) error { return nil }), WithName("ok"))
	if err != nil {
		t.Fatalf("Add ok err=%v", err)
	}
	hFail, err := m.Add(Trigger(func(context.Context) error { return errors.New("x") }), WithName("fail"))
	if err != nil {
		t.Fatalf("Add fail err=%v", err)
	}
	hCancel, err := m.Add(Trigger(func(context.Context) error { return context.Canceled }), WithName("cancel"))
	if err != nil {
		t.Fatalf("Add cancel err=%v", err)
	}

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}
	defer m.Shutdown(context.Background())

	if err := hOK.TriggerAndWait(context.Background()); err != nil {
		t.Fatalf("ok TriggerAndWait err=%v, want nil", err)
	}
	if err := hFail.TriggerAndWait(context.Background()); err == nil {
		t.Fatalf("fail TriggerAndWait err=nil, want non-nil")
	}
	if err := hCancel.TriggerAndWait(context.Background()); err != nil {
		t.Fatalf("cancel TriggerAndWait err=%v, want nil (filtered)", err)
	}

	stOK := hOK.Status()
	if stOK.RunCount != 1 || stOK.SuccessCount != 1 || stOK.FailCount != 0 || stOK.CanceledCount != 0 {
		t.Fatalf("ok counts: run=%d success=%d fail=%d cancel=%d, want 1/1/0/0",
			stOK.RunCount, stOK.SuccessCount, stOK.FailCount, stOK.CanceledCount)
	}

	stFail := hFail.Status()
	if stFail.RunCount != 1 || stFail.SuccessCount != 0 || stFail.FailCount != 1 || stFail.CanceledCount != 0 {
		t.Fatalf("fail counts: run=%d success=%d fail=%d cancel=%d, want 1/0/1/0",
			stFail.RunCount, stFail.SuccessCount, stFail.FailCount, stFail.CanceledCount)
	}

	stCancel := hCancel.Status()
	if stCancel.RunCount != 1 || stCancel.SuccessCount != 0 || stCancel.FailCount != 0 || stCancel.CanceledCount != 1 {
		t.Fatalf("cancel counts: run=%d success=%d fail=%d cancel=%d, want 1/0/0/1",
			stCancel.RunCount, stCancel.SuccessCount, stCancel.FailCount, stCancel.CanceledCount)
	}
}

func TestEvery_FixedDelay_SkippedTick_WaitsIntervalAfterCompletion(t *testing.T) {
	t.Parallel()

	m := NewManager()
	interval := 60 * time.Millisecond

	gate := make(chan struct{})
	start1 := make(chan time.Time, 1)
	finish1 := make(chan time.Time, 1)
	start2 := make(chan time.Time, 1)

	var n atomic.Int64
	_, err := m.Add(Every(interval, func(context.Context) error {
		i := n.Add(1)
		switch i {
		case 1:
			start1 <- time.Now()
			<-gate
			finish1 <- time.Now()
			return nil
		case 2:
			start2 <- time.Now()
			return nil
		default:
			return nil
		}
	}),
		WithEveryMode(EveryFixedDelay),
		WithStartImmediately(true),
		WithMaxConcurrent(1),
		WithOverlapPolicy(OverlapSkip),
	)
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}
	defer m.Shutdown(context.Background())

	select {
	case <-start1:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("first run did not start")
	}

	// Let a few ticks happen while run 1 is in-flight (they will be skipped).
	time.Sleep(2 * interval)

	close(gate)

	var tFinish time.Time
	select {
	case tFinish = <-finish1:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("first run did not finish")
	}

	var tStart2 time.Time
	select {
	case tStart2 = <-start2:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("second run did not start")
	}

	// Fixed-delay: the next scheduled run should not start immediately after completion;
	// it should be roughly interval after the latest completion.
	if dt := tStart2.Sub(tFinish); dt < interval-20*time.Millisecond {
		t.Fatalf("start2-finish1=%s, want >= %s (approx)", dt, interval-20*time.Millisecond)
	}
}

func TestEvery_FixedDelay_FastRuns_NoDeadlock(t *testing.T) {
	t.Parallel()

	m := NewManager()
	var runs atomic.Int64

	interval := 2 * time.Millisecond
	_, err := m.Add(Every(interval, func(context.Context) error {
		runs.Add(1)
		return nil
	}), WithEveryMode(EveryFixedDelay), WithStartImmediately(true))
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}
	defer m.Shutdown(context.Background())

	deadline := time.Now().Add(200 * time.Millisecond)
	for runs.Load() < 5 {
		if time.Now().After(deadline) {
			t.Fatalf("runs=%d, want >=5; fixed-delay scheduler may be stuck", runs.Load())
		}
		time.Sleep(1 * time.Millisecond)
	}
}

func TestTriggerAndWait_ManyTimeouts_DoNotAccumulateWaiters(t *testing.T) {
	t.Parallel()

	m := NewManager()

	gate := make(chan struct{})
	started := make(chan struct{}, 1)
	var runs atomic.Int64

	h, err := m.Add(Trigger(func(context.Context) error {
		i := runs.Add(1)
		if i == 1 {
			select {
			case started <- struct{}{}:
			default:
			}
			<-gate
			return nil
		}
		// merged run(s) should complete quickly
		return nil
	}), WithMaxConcurrent(1), WithOverlapPolicy(OverlapMerge))
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}
	tr := h.(*taskRuntime)

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}
	defer m.Shutdown(context.Background())

	// Start the first run and block it.
	go func() { _ = h.TriggerAndWait(context.Background()) }()
	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("first run did not start")
	}

	const N = 50
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
			defer cancel()
			_ = h.TriggerAndWait(ctx)
		}()
	}
	wg.Wait()

	// Waiters from timed-out callers should not accumulate.
	tr.mu.Lock()
	pn := len(tr.pendingWaiters)
	tr.mu.Unlock()
	if pn != 0 {
		t.Fatalf("pendingWaiters=%d, want 0 after many timeouts", pn)
	}

	// The triggers should still be effective: after releasing the first run, we should see
	// at least one merged run start.
	close(gate)
	deadline := time.Now().Add(500 * time.Millisecond)
	for runs.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("runs=%d, want >=2 after releasing gate", runs.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAdd_ManyCallsWhileStopping_AllErrClosed(t *testing.T) {
	t.Parallel()

	m := NewManager()

	block := make(chan struct{})
	started := make(chan struct{}, 1)
	h, err := m.Add(Trigger(func(context.Context) error {
		select {
		case started <- struct{}{}:
		default:
		}
		<-block
		return nil
	}))
	if err != nil {
		t.Fatalf("Add err=%v", err)
	}

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}

	go func() { _ = h.TriggerAndWait(context.Background()) }()
	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("run did not start")
	}

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- m.Shutdown(context.Background()) }()

	// Wait until stopping.
	deadline := time.Now().Add(200 * time.Millisecond)
	for m.managerState() != managerStopping {
		if time.Now().After(deadline) {
			t.Fatalf("manager did not enter stopping state")
		}
		time.Sleep(1 * time.Millisecond)
	}

	const N = 50
	var wg sync.WaitGroup
	wg.Add(N)
	var ok atomic.Int64
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			_, err := m.Add(Trigger(func(context.Context) error { return nil }))
			if errors.Is(err, ErrClosed) {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != N {
		t.Fatalf("ErrClosed count=%d, want %d", ok.Load(), N)
	}

	close(block)
	_ = <-shutdownDone
}

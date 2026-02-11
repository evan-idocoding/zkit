package task

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStress_ConcurrentTriggerAndShutdown_NoDeadlock(t *testing.T) {
	// This is a "property-ish" test: it tries to exercise concurrency paths without relying
	// on brittle timing assumptions. It must finish quickly or fail.
	m := NewManager()

	// Long-running task to keep manager in stopping state for a while.
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

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}

	// Start the first run so Shutdown will block.
	go func() { _ = h.TriggerAndWait(context.Background()) }()
	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("first run did not start")
	}

	// Concurrently spam triggers (some with timeouts) while shutting down.
	var wg sync.WaitGroup
	var triggerCalls atomic.Int64

	wg.Add(1)
	go func() {
		defer wg.Done()
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			triggerCalls.Add(1)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
			_ = h.TriggerAndWait(ctx)
			cancel()
		}
	}()

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- m.Shutdown(context.Background()) }()

	// Allow shutdown to proceed.
	close(block)

	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatalf("Shutdown err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Shutdown did not complete (potential deadlock)")
	}
	wg.Wait()

	if got := triggerCalls.Load(); got == 0 {
		t.Fatalf("expected some trigger calls")
	}
}

func TestStress_FixedDelaySchedulerAndTriggers_NoDeadlock(t *testing.T) {
	m := NewManager()

	var runs atomic.Int64
	interval := 3 * time.Millisecond

	h, err := m.Add(Every(interval, func(context.Context) error {
		runs.Add(1)
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

	// Mix in manual triggers (they should not deadlock fixed-delay scheduler).
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		deadline := time.Now().Add(120 * time.Millisecond)
		for time.Now().Before(deadline) {
			_ = h.TryTrigger()
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Expect the scheduler to keep making progress.
	deadline := time.Now().Add(300 * time.Millisecond)
	for runs.Load() < 10 {
		if time.Now().After(deadline) {
			t.Fatalf("runs=%d, want >=10; scheduler may be stuck", runs.Load())
		}
		time.Sleep(2 * time.Millisecond)
	}
	wg.Wait()
}

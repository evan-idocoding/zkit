package task_test

import (
	"context"
	"fmt"

	"github.com/evan-idocoding/zkit/rt/task"
)

func ExampleManager_triggerAndWait() {
	m := task.NewManager()
	h := m.MustAdd(task.Trigger(func(context.Context) error {
		fmt.Println("ran")
		return nil
	}), task.WithName("rebuild-index"))

	_ = m.Start(context.Background())
	defer m.Shutdown(context.Background())

	_ = h.TriggerAndWait(context.Background())

	// Output:
	// ran
}

func ExampleManager_snapshot() {
	m := task.NewManager()
	h := m.MustAdd(task.Trigger(func(context.Context) error { return nil }), task.WithName("job"))

	_ = m.Start(context.Background())
	defer m.Shutdown(context.Background())

	_ = h.TriggerAndWait(context.Background())

	snap := m.Snapshot()
	st, ok := snap.Get("job")
	fmt.Println(ok, st.RunCount, st.SuccessCount, st.FailCount, st.CanceledCount)

	// Output:
	// true 1 1 0 0
}

func ExampleManager_shutdownWithoutStart() {
	m := task.NewManager()
	h := m.MustAdd(task.Trigger(func(context.Context) error { return nil }), task.WithName("job"))

	_ = m.Shutdown(context.Background())

	fmt.Println(h.Status().State)

	// Output:
	// stopped
}
